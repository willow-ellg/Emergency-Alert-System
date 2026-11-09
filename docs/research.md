# Research notes

This project started from the proposal document `Interactive Public Defence
Awareness.docx`. The proposal describes a public hub that combines real-time
emergency information, location-specific guidance, misinformation correction
and moderated public reports. It suggested a Rails/PostgreSQL/Sidekiq stack and
pointed at GOV.UK guidance. The implementation keeps the product intent and the
GOV.UK presentation, but uses a smaller Go stack so the thin emergency system
can run without a database or a JavaScript toolchain.

## Sources reviewed

- GOV.UK emergency publishing manual:
  <https://docs.publishing.service.gov.uk/manual/emergency-publishing.html>
- GOV.UK architecture deep dive:
  <https://docs.publishing.service.gov.uk/manual/architecture-deep-dive.html>
- GOV.UK conventions for Rails applications:
  <https://docs.publishing.service.gov.uk/manual/conventions-for-rails-applications.html>
- The twelve-factor app: <https://12factor.net/>
- GOV.UK fall-back-to-mirror guidance:
  <https://docs.publishing.service.gov.uk/manual/fall-back-to-mirror.html>
- GOV.UK asset guidance:
  <https://docs.publishing.service.gov.uk/manual/assets.html>
- GOV.UK Design System:
  <https://github.com/alphagov/govuk-design-system>
- GOV.UK Frontend:
  <https://github.com/alphagov/govuk-frontend>

## Findings and design implications

### Emergency publishing is a process, not just a page

The emergency publishing guidance treats publishing as a rehearsed operational
process: named responders, fallback routes, cached content and clear escalation.
A web application alone is not enough. The implementation therefore includes an
operator console, a documented runbook, an explicit snapshot step and a separate
thin server that can be started by one person.

### Separate the resilient path from the feature-rich path

The fall-back-to-mirror guidance describes serving a static mirror when the
publishing platform is unavailable. The project turns that idea into a first
class component: `cmd/beacon` serves a static directory and has no database,
templates or write path. The hub is allowed to be more capable because the thin
path does not depend on it.

### Prefer boring, portable storage

The proposal suggested PostgreSQL. That is appropriate for a large, multi-team
service, but it adds an operational dependency to the emergency path. A single
JSON file with atomic replacement is sufficient for a prototype and is easy to
copy, inspect, back up and render. The store interface is deliberately small so
it can be replaced later without changing the HTTP or snapshot layers.

### Treat every dependency as a failure mode

The GOV.UK architecture guidance emphasises small, independently deployable
applications and clear boundaries. This implementation uses only the Go standard
library at runtime. The full site embeds its templates and assets, and the
snapshot is self-contained. Nothing is loaded from a CDN. That reduces the number
of things that can fail during an incident.

### Use the real design system, not an imitation

The GOV.UK Design System provides accessible, tested markup and behaviour. The
project vendors GOV.UK Frontend 5.11.1 and uses its classes and component
structure directly. The only custom CSS adds emergency banner colours,
high-contrast and large-text preferences, print rules and small spacing helpers.
This keeps the service familiar to the public and avoids inventing a new visual
language during a crisis.

### Make the trust boundary visible

Emergency information is only useful if people know whether it is official and
current. The UI therefore includes:

- a phase banner that says this is a prototype or a snapshot;
- a demonstration warning whenever demo data is present;
- `noindex,nofollow` on demo pages;
- a source and an `official` flag on every alert;
- a clear distinction between official alerts and verified community updates;
- prominent real emergency contact numbers.

### Accessibility is part of resilience

The design system handles keyboard navigation, focus states, semantic headings
and screen-reader labels. On top of that, the public page offers high-contrast
and larger-text preferences, respects reduced motion, and has print styles that
remove navigation and forms. The snapshot has no scripts at all, so it remains
usable in constrained browsers and when JavaScript is blocked.

### Twelve-factor ideas that still apply

- **Config in the environment**: `HUB_ADMIN_TOKEN`, listen addresses, data paths
  and snapshot paths are flags or environment variables.
- **Stateless processes**: the hub keeps no session state in memory; the beacon
  keeps no state at all.
- **Dev/prod parity**: the same binary and the same snapshot format are used in
  development and production.
- **Logs as event streams**: the binaries log to stdout/stderr for collection by
  the host.
- **Disposability**: both servers shut down gracefully on `SIGINT`/`SIGTERM`.

## Deliberate deviations from the proposal

- Go instead of Rails: the thin system must be a single static binary.
- JSON file instead of PostgreSQL: fewer moving parts on the emergency path.
- Server-Sent Events instead of WebSockets: simpler, one-way updates over plain
  HTTP, with no extra protocol upgrade.
- Static snapshot instead of a second database-backed site: the mirror can be
  copied to any host and served by any static file server.
