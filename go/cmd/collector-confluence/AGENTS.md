# collector-confluence Agent Guidance

## Read First

1. `README.md` and `doc.go` for command scope.
2. `service.go` for `buildCollectorService`, Postgres commit wiring, and env
   handoff.
3. `main.go` for telemetry bootstrap and hosted status.
4. `go/internal/collector/confluence/README.md` for source, pagination,
   permission gaps, metadata, and fact output.
5. `go/internal/collector/README.md` and `go/internal/runtime/README.md` for
   shared service and hosting contracts.

## Local Rules

- Keep Confluence access read-only. Do not add write/update APIs or mutation
  credentials.
- Require exactly one bounded source mode: one space, an explicit space-ID
  allowlist, or one root page tree. Blank config must not become site-wide
  crawling.
- Emit documentation evidence through `collector.Service` and
  `postgres.NewIngestionStore`; do not write facts directly from command code.
- Keep the hosted status server and Prometheus handler wired.
- Pass shared telemetry into the Confluence HTTP client and source so source
  metrics reach `/metrics`.
- Keep page titles, body content, excerpts, URLs, paths, and credentials out of
  metric labels, status details, and docs.

## Change Rules

- Add config in `internal/collector/confluence`, cover it with config tests,
  thread it through `buildCollectorService`, and update package docs.
- Change page collection only with tests for empty spaces, permission gaps,
  stale revisions, duplicate titles, pagination, and partial syncs.
- Keep Confluence metric labels bounded to the documented operation, result,
  status class, and failure class dimensions.
