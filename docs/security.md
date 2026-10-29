# Security review

## Authentication and sessions

- Single administrator; passwords PBKDF2-SHA256 with 600k iterations and a
  random salt.
- Server-side sessions; HttpOnly, SameSite=Lax cookies; `Secure` controlled by
  `AIPTIMIZE_COOKIE_SECURE`; 30-day idle expiry; logout revokes the session.
- CSRF double-submit token on every state-changing request; origin checked when
  `Origin` is present.
- Login limited to five attempts per minute per client IP.

## Headers and network

- CSP `default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self'
  data:; connect-src 'self'`; no CORS.
- `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`,
  `X-Frame-Options: DENY`, restrictive `Permissions-Policy`.
- Intended for a trusted LAN behind an optional TLS reverse proxy. Do not expose
  port 8080 directly to the internet.

## Gates and audit

- Inverter writes require `AIPTIMIZE_ENABLE_WRITES=true` and the UI arm switch.
- EV control requires `AIPTIMIZE_ENABLE_EV_CONTROL=true` and its own UI switch.
- Both are disarmed by default. Applying a plan or EV command also requires
  `confirm: true`; plan revisions are checked optimistically.
- Plan drafts/applies, schedule writes/readbacks, EV commands, clear and
  takeover are audited in SQLite.

## Secrets

- Zaptec credentials, session/admin secrets and the Nord Pool/Open-Meteo URLs
  are environment-only. They are never returned by the API or written to logs.
- Settings export contains configuration only, not credentials or session keys.
- Backups contain the SQLite database, which includes password hashes and
  sessions; treat the volume as sensitive.

## Firmware / HIL

Real inverter and Zaptec writes have not been HIL-signed-off. Keep both gates
off until the HIL checklist in `DEVELOPMENT_PLAN.MD` passes. The simulator is the
only writable target used by tests and the demo.

## Data retention

Telemetry is pruned after 90 days, events after one year; sessions are pruned
when expired. Backups should be taken after a WAL checkpoint (see
`deployment.md`).
