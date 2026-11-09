# Emergency Alert System

A public defence awareness and emergency response hub, plus a thin standalone
snapshot system that can keep publishing information when the main platform is
unavailable.

> **Prototype only.** This repository contains demonstration data and is not
> connected to the national alerting system. It is not a government service and
> must not be used as the only source of information in a real emergency. Call
> **999** if there is an immediate risk to life.

## What is here

The project has two deployables that share a visual language but have no runtime
dependency on each other:

- **Hub** (`bin/hub`) — the full service. It stores alerts, public reports and
  myth-busting corrections in a single JSON file, moderates submissions, streams
  live updates over Server-Sent Events, and exports a static snapshot.
- **Beacon** (`bin/beacon`) — the thin service. It serves a pre-generated
  snapshot directory with no database, no templates and no writes. It also has
  small commands for adding or removing alerts during an incident.

The UI is built with the unmodified **GOV.UK Frontend 5.11.1** stylesheet and
the GOV.UK Design System markup, including the header, footer, phase banner,
warning text, notification banners, tags, summary cards and form patterns.

## Quick start

Nix is the only prerequisite. The shell provides Go and Make.

```sh
nix-shell
make build
make run
```

Open <http://localhost:8080>. The hub seeds clearly labelled demonstration data
on first start. The operator area is at <http://localhost:8080/admin> and uses
the development token `dev-token`.

Generate and serve the standalone snapshot:

```sh
make snapshot
make beacon
```

Open <http://localhost:8081>. The snapshot is a directory of static files and
can also be served by nginx, Caddy, Apache or any other static file server.

## Commands

| Command | Purpose |
| --- | --- |
| `make build` | Build `bin/hub` and `bin/beacon`. |
| `make test` | Run the Go test suite. |
| `make vet` | Run `go vet`. |
| `make run` | Build and start the hub on `:8080` with `HUB_ADMIN_TOKEN=dev-token`. |
| `make seed` | Seed demonstration data without starting the server. |
| `make snapshot` | Render `./snapshot` from `./data/state.json`, or from demo data if the state file is empty. |
| `make beacon` | Serve `./snapshot` on `:8081`. |
| `make snapshot DEMO=false` | Render without the demonstration warning, for verified live data. |
| `make clean` | Remove `bin/`, `data/` and `snapshot/`. |

Useful environment variables and flags:

```sh
HUB_ADMIN_TOKEN=... bin/hub \
  -addr :8080 \
  -data /var/lib/emergency-alert-system/state.json \
  -snapshot-dir /var/lib/emergency-alert-system/snapshot \
  -demo=false
```

`-demo=false` hides the demonstration warning. It is refused when the hub has
just seeded demonstration data. Only use it with real, verified content.

## Operator API

The operator API is disabled unless `HUB_ADMIN_TOKEN` is set. Send the token as
`Authorization: Bearer <token>` or `X-Admin-Token: <token>`.

```sh
# Publish an alert
curl -X POST http://localhost:8080/api/admin/alerts \
  -H "Authorization: Bearer dev-token" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Flood warning: Riverside",
    "severity": "critical",
    "summary": "River levels are rising. Move to higher ground.",
    "guidance": ["Avoid flood water", "Check on neighbours"],
    "areas": ["Riverside", "Lowbridge"],
    "source": "Environment Agency",
    "official": true,
    "active": true
  }'

# Moderate a pending report
curl -X POST http://localhost:8080/api/admin/reports/REPORT_ID/approve \
  -H "Authorization: Bearer dev-token" \
  -H "Content-Type: application/json" \
  -d '{"verified": true}'

# Generate the standalone snapshot
curl -X POST http://localhost:8080/api/admin/snapshot \
  -H "Authorization: Bearer dev-token"
```

Public endpoints:

- `GET /api/alerts` — active alerts, optionally filtered with `area`, `lat` and `lon`.
- `GET /api/alerts/{id}` — one active alert.
- `GET /api/reports` — approved community updates, optionally filtered with `area` or `alert_id`.
- `POST /api/reports` — submit a report for moderation.
- `GET /api/myths` — corrected claims.
- `GET /api/stream` — Server-Sent Events stream; emits `ready` and `update` events.
- `GET /api/health` — health and dataset counts.

## Emergency operation

The thin system is designed to be operated by one person under pressure.

1. On the hub, generate a snapshot:

   ```sh
   bin/hub ... # running normally
   curl -X POST http://hub/api/admin/snapshot -H "Authorization: Bearer $HUB_ADMIN_TOKEN"
   ```

   Or render one directly from a state file:

   ```sh
   bin/beacon render -dir ./snapshot -data ./data/state.json -demo=false
   ```

2. Copy the snapshot directory to the isolated host. It is self-contained:
   `index.html`, `site.css`, the GOV.UK CSS, fonts, images, `data/alerts.json`
   and `data/myths.json`.

3. Start the beacon:

   ```sh
   bin/beacon serve -dir ./snapshot -addr :8081
   ```

4. Verify it:

   ```sh
   curl http://localhost:8081/api/health
   curl http://localhost:8081/api/alerts
   ```

5. If you need to change the published information and the hub is unavailable,
   edit the snapshot directly:

   ```sh
   bin/beacon add -dir ./snapshot \
     -title "Flood warning: Riverside" \
     -severity critical \
     -summary "River levels are rising. Move to higher ground." \
     -areas "Riverside, Lowbridge" \
     -guidance "Avoid flood water|Check on neighbours" \
     -source "Environment Agency"

   bin/beacon list -dir ./snapshot
   bin/beacon remove -dir ./snapshot -id alert-...
   ```

   Each edit rewrites the static page, so the beacon keeps serving a valid
   snapshot throughout.

The beacon is read-only over HTTP. It never accepts reports, never exposes
pending or rejected moderation data, and has no admin token.

## Security and privacy

- All state is stored in one JSON file. Writes use a temporary file, `fsync`
  and atomic rename.
- The hub sets a strict Content Security Policy with no inline scripts. All
  JavaScript is served as separate files from the same origin.
- Admin requests use a constant-time token comparison and are disabled when no
  token is configured.
- Public report submissions are rate limited per client address and held for
  moderation. Only approved reports are published. Behind a trusted reverse
  proxy, pass `-trust-proxy` so the proxy's `X-Forwarded-For` or `X-Real-IP`
  header is used; never enable it when the hub is directly reachable.
- Reports should never contain names, contact details or full addresses. The
  public form says this explicitly.
- The snapshot excludes pending and rejected reports by design.
- The beacon sends `script-src 'none'` and never executes application code.

For production, terminate TLS in front of the hub, keep the state file readable
only by the service account, back it up regularly, and place the beacon on a
separate host or network path from the hub.

## Project layout

```text
cmd/hub/            full hub entrypoint
cmd/beacon/         thin beacon entrypoint
internal/model/     shared domain types and location matching
internal/store/     atomic JSON datastore
internal/seed/      clearly labelled demonstration data
internal/hub/       public and operator HTTP surface, SSE, snapshot export
internal/beacon/    read-only snapshot server and snapshot editing
internal/snapshot/  static snapshot renderer
internal/view/      shared template helpers
web/templates/      GOV.UK Design System markup
web/static/         application CSS/JS and vendored GOV.UK Frontend assets
deploy/             example systemd units
docs/               research notes, architecture and decisions
```

## Testing

```sh
nix-shell --run 'go test ./...'
nix-shell --run 'go vet ./...'
nix-shell --run 'CGO_ENABLED=1 go test -race ./...'
```

The tests cover location matching, datastore persistence and cloning, report
moderation, admin authentication, rate limiting, snapshot rendering, snapshot
asset rewriting, beacon serving and the broker.

## Licence

Application code is provided for this prototype. GOV.UK Frontend is MIT
licensed; see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
