# ADR 0001 — One Go binary with an embedded Vue SPA

- Status: accepted
- Date: 2026-10-29

## Context

Aiptimize is a single-site appliance deployed with Docker on a home LAN. It must
be easy to install, upgrade and back up, and it must keep working when the
internet is unavailable. The UI is mobile-first and iterative, so a modern
component toolchain is desirable.

## Decision

Build the UI with Vue 3 + TypeScript + Vite and embed the production build in
the Go binary with `//go:embed`. The Go server serves both `/api/v1` and the SPA;
there is no separate frontend server, reverse proxy requirement or CORS layer.

## Consequences

- One artifact and one process to operate; the UI version always matches the
  API.
- The UI must be rebuilt before `go build`; the Makefile and Dockerfile encode
  this. A binary built without assets serves a clear placeholder page.
- Frontend hot reload runs on Vite with a proxy to the Go server during
  development.
- Cache busting relies on hashed Vite asset filenames; `index.html` is served
  with `no-cache`.
