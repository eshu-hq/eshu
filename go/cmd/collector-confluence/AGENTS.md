# AGENTS.md - cmd/collector-confluence guidance for LLM assistants

## Read First

1. `go/cmd/collector-confluence/README.md` - binary purpose, config, and
   invariants
2. `go/cmd/collector-confluence/service.go` - `buildCollectorService` and env
   wiring
3. `go/internal/collector/confluence/` - Confluence source, config, and HTTP
   client
4. `go/internal/collector/README.md` - shared `collector.Service` contract
5. `go/internal/runtime/README.md` - shared Postgres and hosted runtime setup

## Invariants This Package Enforces

- **Read-only Confluence access** - the HTTP client must only issue `GET`
  requests. Eshu gathers evidence; it does not mutate Confluence.
- **Bounded collection** - startup must require exactly one bounded source:
  a Confluence space ID or a root page ID.
- **Shared durable write boundary** - facts must flow through
  `collector.Service` and `postgres.NewIngestionStore`.
- **Operator visibility** - keep the hosted status server and Prometheus
  handler wired into `app.NewHostedWithStatusServer`.

## Common Changes And How To Scope Them

- **Add a config option** - update `confluence.LoadConfig`, add config tests,
  thread the value through `buildCollectorService`, and update `README.md`.
- **Change page collection behavior** - add source tests for empty spaces,
  permission gaps, stale revisions, and duplicate titles before changing
  `internal/collector/confluence`.
- **Change telemetry** - verify emitted collector metrics keep
  `collector_kind=documentation` and `source_system=confluence`.

## Anti-Patterns

- Writing documentation facts directly from `main.go` or `service.go`.
- Adding Confluence write calls, update APIs, or mutation credentials.
- Treating permission gaps as complete syncs.
