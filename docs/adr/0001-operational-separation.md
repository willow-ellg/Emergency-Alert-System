# ADR 0001: Separate the thin emergency system from the hub

- Status: Accepted
- Date: 2026-11-09

## Context

The proposal describes a feature-rich public hub with alerts, location-aware
guidance, misinformation correction and moderated public reports. It also asks
for a system that can operate efficiently during an emergency and that can run
independently of the main government website.

A single application with a database, background workers and a JavaScript build
pipeline has many failure points. During an incident, the public still needs
read-only information even if the database, queue, admin service or network path
to the main platform is unavailable.

## Decision

Build two deployables from one repository:

1. **Hub** — the full service. It owns the JSON datastore, moderation, live
   updates and snapshot export.
2. **Beacon** — a thin, standalone server. It serves a pre-generated static
   snapshot directory and has no database, templates or write path over HTTP.

The beacon shares domain types, template helpers and embedded assets with the
hub, but it has no runtime dependency on the hub. The snapshot is a plain
directory that can be copied to an isolated host and served by the beacon or by
any static file server.

## Consequences

Positive:

- The public information path has very few moving parts.
- A snapshot can be reviewed, copied, archived and served offline.
- The beacon can be run by one operator under pressure.
- Pending and rejected reports are excluded from the snapshot by construction.
- The hub can evolve without making the emergency path more fragile.

Negative:

- There are two binaries and two deployment units to operate.
- The snapshot is a point-in-time copy; it does not update automatically.
- Live editing of a snapshot requires an explicit `beacon add` or `beacon
  remove` command.
- The hub and beacon can diverge if the snapshot is not regenerated. The UI
  makes the snapshot state and generation time visible.

## Alternatives considered

- **One Rails application with a read-only mode.** Rejected because the
  read-only mode would still depend on the database, the Rails runtime and the
  asset pipeline.
- **A second database-backed application.** Rejected because it duplicates the
  operational surface and still fails when the database is unavailable.
- **A CDN-only mirror.** Useful as an additional layer, but it does not give an
  operator a local, inspectable copy that can be edited during an incident.
