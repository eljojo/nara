---
title: Deployment
---

# Deployment

## Purpose

Deployment describes how to build and run Nara binaries and UI assets.
It documents the supported build targets and the public web deployment config.

## Conceptual Model

Entities:
- `bin/nara`: main runtime with embedded UI and docs.
- `bin/nara-backup`: backup/import helper tool.
- `nara-web/public`: built UI assets bundled into the binary.

Key invariants:
- Web assets are built before the main binary.
- Docs are built into `nara-web/public/docs`.
- The runtime expects a single HTTP port for both UI and APIs.

## External Behavior

Build targets:
- `make build-web`: builds the UI bundle and docs, then copies into `nara-web/public`.
- `make build`: builds `bin/nara` (depends on build-web).
- `make build-backup`: builds `bin/nara-backup`.
- `make clean`: removes build artifacts.
- `make build-nix`: builds via Nix.

Runtime examples (Makefile):
- `make run`: starts Nara with UI on `:8080`.
- `make run2`: starts a second instance on `:8081` with `-nara-id nixos`.

## Interfaces

Makefile targets:
- `build-web`, `build`, `build-backup`, `run`, `run2`, `run3`, `clean`, `build-nix`.

Fly.io deployment:
- `fly.toml` defines app `nara-web`.
- `PORT=8080` with HTTP service bound to internal 8080.
- Health check: `GET /` every 15s.

## Event Types & Schemas (if relevant)

- None.

## Algorithms

Build pipeline:
1. `npm run build` compiles the SPA assets.
2. `astro build` compiles the docs site.
3. Docs are copied into `nara-web/public/docs`.
4. Go build embeds assets into the binary.

## Failure Modes

- Missing UI build artifacts result in empty or missing web UI.
- Missing docs build artifacts result in `/docs/` returning 404.

## Security / Trust Model (if relevant)

- Deployment is standard single-binary; access control is external.

## Test Oracle

- Build target produces `bin/nara` with UI assets. (manual build)
- Fly config exposes `/` on port 8080. (`fly.toml`)

## Open Questions / TODO

- None.
