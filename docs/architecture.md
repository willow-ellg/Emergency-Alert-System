# Architecture

## Components

```text
                       +------------------------------+
                       |           Hub                |
  public browser ----> |  Go net/http                 |
                       |  templates + GOV.UK Frontend |
  operator browser --> |  JSON store + SSE            |
                       |  moderation + admin API      |
                       +---------------+--------------+
                                       |
                                       | snapshot.Write()
                                       v
                       +------------------------------+
                       |   Static snapshot directory  |
                       |   index.html                 |
                       |   govuk/govuk-frontend.css   |
                       |   site.css, fonts, images    |
                       |   data/alerts.json           |
                       |   data/myths.json            |
                       |   snapshot.json              |
                       +---------------+--------------+
                                       |
                                       | copied to an isolated host
                                       v
                       +------------------------------+
  public browser ----> |          Beacon              |
                       |  Go net/http file server     |
                       |  read-only, no database      |
                       |  no templates, no writes     |
                       +------------------------------+
```

The hub and the beacon share domain types (`internal/model`), template helpers
(`internal/view`) and the embedded assets (`web`). They do not share a database,
a process, a network connection or an admin credential.

## Hub

`cmd/hub` loads a JSON state file, seeds demonstration data if the file is
empty, and starts `internal/hub.Server`.

### Public surface

- `GET /` renders the public page with active alerts, approved reports,
  misinformation corrections, emergency contacts and a report form.
- `GET /api/alerts` returns active alerts. The `area`, `lat` and `lon` query
  parameters filter the list. With no filter, every active alert is returned.
  A national alert matches every filter.
- `GET /api/alerts/{id}` returns one active alert.
- `GET /api/reports` returns approved reports.
- `POST /api/reports` accepts a JSON report and queues it as `pending`.
- `POST /reports` is the no-JavaScript form fallback. It redirects back to the
  public page with a success or error message.
- `GET /api/myths` returns corrected claims.
- `GET /api/stream` is a Server-Sent Events stream. The server sends `ready`
  once, `update` when an alert or moderation decision changes, and a comment
  every 25 seconds to keep proxies from closing the connection.
- `GET /partials/alerts` and `GET /partials/reports` return HTML fragments used
  by the live-update script.
- `GET /api/health` reports the mode, version and dataset counts.

### Operator surface

All operator routes require `Authorization: Bearer <HUB_ADMIN_TOKEN>` or
`X-Admin-Token`. If the token is empty, the routes return `503` rather than
silently allowing access.

- `GET /api/admin/alerts` returns every alert, including inactive ones.
- `POST /api/admin/alerts` creates or updates an alert. An omitted `id` creates
  a new alert.
- `DELETE /api/admin/alerts/{id}` removes an alert.
- `GET /api/admin/reports?status=pending|approved|rejected|all` lists reports.
- `POST /api/admin/reports/{id}/approve` and `.../reject` moderate a report.
- `POST /api/admin/myths` creates or updates a correction.
- `POST /api/admin/snapshot` renders the static snapshot.
- `GET /admin` serves the operator console. The console stores the token in
  `sessionStorage` for the current tab only.

### Storage

`internal/store` keeps a `model.State` in memory and persists it to one JSON
file. Every write:

1. increments `version`;
2. sets `updated_at`;
3. writes to a temporary file in the same directory;
4. sets `0600` permissions;
5. calls `fsync`;
6. atomically renames over the old file.

Readers receive deep copies, so callers cannot mutate store state by accident.

### Live updates

The hub keeps an in-memory broker with a buffered channel per connected client.
Publishing never blocks: if a client is slow, the update is dropped and the
client's next refresh or reconnect catches up. This favours availability over
strict delivery, which is appropriate for a public information page.

## Snapshot

`internal/snapshot.Write` produces a directory that can be served by anything:

- `index.html` — fully rendered, no scripts, no forms, printable.
- `site.css` — application overrides.
- `govuk/govuk-frontend.min.css` — vendored GOV.UK Frontend stylesheet with
  absolute `/assets/fonts/` and `/assets/images/` URLs rewritten to relative
  `../fonts/` and `../images/` URLs.
- `fonts/` and `images/` — vendored GOV.UK Frontend assets.
- `manifest.json` — web app manifest.
- `data/alerts.json` — active alerts plus generation metadata.
- `data/myths.json` — corrected claims for the thin editing commands.
- `snapshot.json` — machine-readable generation metadata.

Files are replaced one at a time with atomic writes. `index.html` is written
last, so a running beacon keeps serving a complete page while a new snapshot is
being generated. Pending and rejected reports are never included.

## Beacon

`cmd/beacon serve` serves a snapshot directory with `http.FileServer` and a small
read-only API:

- `GET /` — the snapshot page, or `503` if no snapshot exists.
- `GET /api/alerts` — the active alert array.
- `GET /api/health` — generation metadata and readiness.
- Everything else — a static file from the snapshot directory.

The beacon rejects dotfile paths and directory listings, sets `script-src
'none'`, and never writes to disk. Its editing commands (`render`, `add`,
`remove`, `list`) run offline and rewrite the snapshot atomically.

## Failure modes

| Failure | Behaviour |
| --- | --- |
| Hub process stops | Existing snapshots keep working on the beacon. |
| Hub database file is corrupted | Hub refuses to start and logs a parse error; the beacon is unaffected. |
| Snapshot is missing or incomplete | Beacon returns `503` with a clear message; `/api/health` reports `empty`. |
| Snapshot generation is interrupted | The previous `index.html` remains; new files are written atomically. |
| A client disconnects from SSE | The broker drops that client; other clients continue. |
| A client floods reports | The per-address token bucket returns `429` with `Retry-After`. |
| Admin token is missing | Operator API returns `503`; public service still works. |
| Admin token is wrong | Operator API returns `401`; no data is changed. |
| Demo data is present | Public pages show a warning and `noindex,nofollow`. |

## Security boundaries

- Public users can read alerts, reports and corrections, and can create pending
  reports. They cannot read pending or rejected reports.
- Operators can read everything and change state. They are authenticated by a
  shared bearer token in this prototype. A production deployment should replace
  this with an identity provider, per-user accounts and audit logging.
- The beacon has no write surface over HTTP. Anyone who can read the snapshot
  directory can serve it, which is intentional for an emergency mirror.
- TLS, network policy, backups and host hardening are deployment concerns, not
  application features. See `deploy/` for example units.
