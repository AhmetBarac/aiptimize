# ADR 0002 — SQLite with modernc.org/sqlite

- Status: accepted
- Date: 2026-10-29

## Context

The application stores settings, price history, telemetry, plans and audit
records for a single installation. It must run in a small Alpine container
without a C toolchain and survive restarts and power loss.

## Decision

Use SQLite in WAL mode through the pure-Go `modernc.org/sqlite` driver, with
embedded, versioned SQL migrations. Use one pooled connection to keep PRAGMAs
deterministic and serialise writes. Checkpoint the WAL on graceful shutdown.

## Consequences

- CGO is not required; the image is small and portable.
- No database server to operate or secure.
- One writer at a time is fine for this workload (a handful of rows per minute).
- Backups are file copies after a checkpoint.
- The full v1 schema is created by migration `0001_init.sql`; later phases add
  data through the same repositories, with new migrations where needed.
