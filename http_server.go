package nara

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

//go:embed all:nara-web/public
var staticContent embed.FS

// responseLogger wraps ResponseWriter to capture status code
type responseLogger struct {
	http.ResponseWriter
	status int
}

func (rl *responseLogger) WriteHeader(code int) {
	rl.status = code
	rl.ResponseWriter.WriteHeader(code)
}

// Flush implements http.Flusher interface by delegating to underlying ResponseWriter
func (rl *responseLogger) Flush() {
	if f, ok := rl.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// loggingMiddleware wraps an http.HandlerFunc with request/response logging
func (network *Network) loggingMiddleware(path string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Wrap response writer to capture status
		wrapped := &responseLogger{ResponseWriter: w, status: 200}
		handler(wrapped, r)

		// Batch log the request (aggregated every 3s by LogService)
		if network.logService != nil {
			network.logService.BatchHTTP(r.Method, path, wrapped.status)
		}
	}
}

func (network *Network) startHttpServer(httpAddr string) error {
	listen_interface := httpAddr
	if listen_interface == "" {
		listen_interface = ":8080"
	}

	listener, err := net.Listen("tcp", listen_interface)
	if err != nil {
		return fmt.Errorf("listen error: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	logrus.Printf("Listening for HTTP on port %d", port)

	// Create a mux for handlers (so we can reuse with mesh server)
	mux := network.createHTTPMux(true) // includeUI = true

	network.httpServer = &http.Server{
		Handler: mux,
	}

	go func() {
		if err := network.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			logrus.WithError(err).Error("HTTP server error")
		}
	}()
	return nil
}

// createHTTPMux creates an HTTP mux with all handlers
// includeUI: whether to include web UI handlers (false for mesh-only server)
func (network *Network) createHTTPMux(includeUI bool) *http.ServeMux {
	mux := http.NewServeMux()
	var publicFS fs.FS

	// Mesh endpoints - available on both local and mesh servers
	// These require Ed25519 authentication (except /ping which needs to be fast)
	mux.HandleFunc("/events/sync", network.loggingMiddleware("/events/sync", network.meshAuthMiddleware("/events/sync", network.httpEventsSyncHandler)))
	mux.HandleFunc("/gossip/zine", network.loggingMiddleware("/gossip/zine", network.meshAuthMiddleware("/gossip/zine", network.httpGossipZineHandler)))
	mux.HandleFunc("/dm", network.loggingMiddleware("/dm", network.meshAuthMiddleware("/dm", network.httpDMHandler)))
	mux.HandleFunc("/world/relay", network.loggingMiddleware("/world/relay", network.meshAuthMiddleware("/world/relay", network.httpWorldRelayHandler)))
	mux.HandleFunc("/ping", network.loggingMiddleware("/ping", network.httpPingHandler)) // No auth - latency critical
	mux.HandleFunc("/coordinates", network.loggingMiddleware("/coordinates", network.httpCoordinatesHandler))

	// Checkpoint sync endpoint - serves all checkpoints for boot recovery
	// Available on mesh for distributed timeline recovery
	mux.HandleFunc("/api/checkpoints/all", network.httpCheckpointsAllHandler)

	// Event import endpoint - allows owner to restore events from backup
	// No mesh auth required - uses soul-based signature verification
	mux.HandleFunc("/api/events/import", network.loggingMiddleware("/api/events/import", network.httpEventsImportHandler))

	if includeUI {
		// Prepare static FS
		var err error
		publicFS, err = fs.Sub(staticContent, "nara-web/public")
		if err != nil {
			logrus.Errorf("failed to load embedded UI assets: %v", err)
		}

		// Profile pages: serve SPA for /nara/{name}
		mux.HandleFunc("/nara/", func(w http.ResponseWriter, r *http.Request) {
			if data, err := fs.ReadFile(staticContent, "nara-web/public/inspector.html"); err == nil {
				http.ServeContent(w, r, "inspector.html", time.Now(), bytes.NewReader(data))
				return
			}
			http.NotFound(w, r)
		})
		// Profile JSON data: /profile/{name}.json
		mux.HandleFunc("/profile/", network.httpProfileJsonHandler)

		// Web UI endpoints - only on local server
		mux.HandleFunc("/api.json", network.httpApiJsonHandler)
		mux.HandleFunc("/narae.json", network.httpNaraeJsonHandler)
		mux.HandleFunc("/metrics", network.httpMetricsHandler)
		mux.HandleFunc("/status/", network.httpStatusJsonHandler)
		mux.HandleFunc("/events", network.httpEventsSSEHandler)
		mux.HandleFunc("/social/clout", network.httpCloutHandler)
		mux.HandleFunc("/social/recent", network.httpRecentEventsHandler)
		mux.HandleFunc("/social/teases", network.httpTeaseCountsHandler)
		mux.HandleFunc("/world/start", network.httpWorldStartHandler)
		mux.HandleFunc("/world/journeys", network.httpWorldJourneysHandler)
		mux.HandleFunc("/network/map", network.httpNetworkMapHandler)
		mux.HandleFunc("/proximity", network.httpProximityHandler)

		// Stash API endpoints
		mux.HandleFunc("/api/stash/status", network.httpStashStatusHandler)
		mux.HandleFunc("/api/stash/update", network.httpStashUpdateHandler)
		mux.HandleFunc("/api/stash/recover", network.httpStashRecoverHandler)
		mux.HandleFunc("/api/stash/confidants", network.httpStashConfidantsHandler)

		// Inspector API endpoints
		mux.HandleFunc("/api/inspector/events", network.local.inspectorEventsHandler)
		mux.HandleFunc("/api/inspector/checkpoints", network.local.inspectorCheckpointsHandler)
		mux.HandleFunc("/api/inspector/checkpoint/", network.local.inspectorCheckpointDetailHandler)
		mux.HandleFunc("/api/inspector/projections", network.local.inspectorProjectionsHandler)
		mux.HandleFunc("/api/inspector/projection/", network.local.inspectorProjectionDetailHandler)
		mux.HandleFunc("/api/inspector/event/", network.local.inspectorEventDetailHandler)
		mux.HandleFunc("/api/inspector/uptime/", network.local.inspectorUptimeHandler)

		// SPA handler - serves inspector.html for all app routes
		// This is the main page at / with all tabs (Home, World, Timeline, etc.)
		spaHandler := func(w http.ResponseWriter, r *http.Request) {
			if data, err := fs.ReadFile(staticContent, "nara-web/public/inspector.html"); err == nil {
				http.ServeContent(w, r, "inspector.html", time.Now(), bytes.NewReader(data))
				return
			}
			http.NotFound(w, r)
		}

		// SPA routes - all these serve inspector.html, React router handles the rest
		mux.HandleFunc("/world", spaHandler)
		mux.HandleFunc("/world/", spaHandler)
		mux.HandleFunc("/timeline", spaHandler)
		mux.HandleFunc("/timeline/", spaHandler)
		mux.HandleFunc("/checkpoints", spaHandler)
		mux.HandleFunc("/checkpoints/", spaHandler)
		mux.HandleFunc("/projections", spaHandler)
		mux.HandleFunc("/projections/", spaHandler)
		mux.HandleFunc("/events/", spaHandler)
		// Legacy inspector route redirects to root
		mux.HandleFunc("/inspector", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/", http.StatusMovedPermanently)
		})
		mux.HandleFunc("/inspector/", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/", http.StatusMovedPermanently)
		})

		// Static file server with SPA fallback for root
		// First try to serve static files, then fall back to SPA
		staticHandler := http.FileServer(http.FS(publicFS))
		if docsFS, err := fs.Sub(publicFS, "docs"); err == nil {
			docsHandler := http.StripPrefix("/docs/", http.FileServer(http.FS(docsFS)))
			mux.Handle("/docs/", docsHandler)
			mux.HandleFunc("/docs", func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/docs/", http.StatusMovedPermanently)
			})
		}
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			// If path is exactly "/" or doesn't match a static file, serve SPA
			if r.URL.Path == "/" || r.URL.Path == "/home" {
				spaHandler(w, r)
				return
			}

			// Check if it's a static file (CSS, JS, etc.)
			// Try to open the file - if it exists, serve it
			filePath := strings.TrimPrefix(r.URL.Path, "/")
			if _, err := fs.Stat(publicFS, filePath); err == nil {
				staticHandler.ServeHTTP(w, r)
				return
			}

			// Not a static file, serve SPA (for client-side routing)
			spaHandler(w, r)
		})
	}

	return mux
}

// startMeshHttpServer starts an HTTP server on the tsnet interface for mesh communication
func (network *Network) startMeshHttpServer(tsnetServer interface {
	Listen(string, string) (net.Listener, error)
}) error {
	listener, err := tsnetServer.Listen("tcp", fmt.Sprintf(":%d", DefaultMeshPort))
	if err != nil {
		return fmt.Errorf("failed to listen on tsnet: %w", err)
	}

	logrus.Printf("🕸️  Mesh HTTP server listening on port %d (Tailscale interface)", DefaultMeshPort)

	// Create a mux with mesh-only endpoints (no UI)
	mux := network.createHTTPMux(false)

	network.meshHttpServer = &http.Server{
		Handler: mux,
	}

	go func() {
		if err := network.meshHttpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			logrus.WithError(err).Error("Mesh HTTP server error")
		}
	}()
	return nil
}
