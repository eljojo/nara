// nara - a friendly network
// Main entry point

import { render } from 'preact';

// Router
import { useLocation, matchRoute, Link } from './router.jsx';

// Views
import { HomeView } from './HomeView.jsx';
import { MapView } from './NetworkRadar.jsx';
import { PostcardsView } from './PostcardsView.jsx';
import { ProfileView } from './ProfileView.jsx';
import { TimelineView, EventDetailPage } from './TimelineView.jsx';
import { ProjectionExplorer } from './ProjectionsView.jsx';

// Effects
import { ShootingStarContainer } from './ShootingStars.jsx';

// ============================================================================
// Main App Component
// ============================================================================

function App() {
  const [location] = useLocation();

  const getActiveTab = () => {
    if (location === '/' || location === '/home') return 'home';
    if (location.startsWith('/map')) return 'map';
    if (location.startsWith('/postcards')) return 'postcards';
    if (location.startsWith('/timeline') || location.startsWith('/events')) return 'timeline';
    if (location.startsWith('/projections')) return 'projections';
    return 'home';
  };

  const activeTab = getActiveTab();

  const renderContent = () => {
    let params;

    // Profile page
    params = matchRoute('/nara/:name', location);
    if (params) return <ProfileView name={params.name} />;

    // Event detail
    params = matchRoute('/events/:id', location);
    if (params) return <EventDetailPage eventId={params.id} />;

    // Uptime timeline for specific subject
    params = matchRoute('/projections/uptime/:subject', location);
    if (params) return <ProjectionExplorer initialUptimeSubject={params.subject} />;

    // Projections with tab
    params = matchRoute('/projections/:tab', location);
    if (params) return <ProjectionExplorer initialTab={params.tab} />;

    // Projections list
    if (location.startsWith('/projections')) return <ProjectionExplorer />;

    // Timeline
    if (location.startsWith('/timeline') || location.startsWith('/events')) return <TimelineView />;

    // Postcards
    if (location.startsWith('/postcards')) return <PostcardsView />;

    // Map
    if (location.startsWith('/map')) return <MapView />;

    // Default: Home
    return <HomeView />;
  };

  return (
    <div className="nara-app">
      <ShootingStarContainer />

      <nav className="main-nav">
        <Link href="/" className="brand-link">nara</Link>
        <div className="nav-tabs">
          <Link href="/">
            <button className={`nav-tab ${activeTab === 'home' ? 'active' : ''}`}>
              <i className="iconoir-home"></i> Home
            </button>
          </Link>
          <Link href="/map">
            <button className={`nav-tab ${activeTab === 'map' ? 'active' : ''}`}>
              <i className="iconoir-map"></i> Map
            </button>
          </Link>
          <Link href="/postcards">
            <button className={`nav-tab ${activeTab === 'postcards' ? 'active' : ''}`}>
              <i className="iconoir-mail"></i> Postcards
            </button>
          </Link>
          <Link href="/timeline">
            <button className={`nav-tab ${activeTab === 'timeline' ? 'active' : ''}`}>
              <i className="iconoir-calendar"></i> Timeline
            </button>
          </Link>
          <Link href="/projections">
            <button className={`nav-tab ${activeTab === 'projections' ? 'active' : ''}`}>
              <i className="iconoir-stats-up-square"></i> Projections
            </button>
          </Link>
        </div>
        <div className="nav-links">
          <a href="/docs">Documentation</a>
          <a href="/stash.html">Stash</a>
          <a href="/resources.html">Monitor</a>
        </div>
      </nav>

      <main className="main-content">
        {renderContent()}
      </main>
    </div>
  );
}

// ============================================================================
// Render
// ============================================================================

const container = document.getElementById('app_root');
if (container) {
  render(<App />, container);
}
