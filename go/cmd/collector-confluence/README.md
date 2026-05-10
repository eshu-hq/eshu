# collector-confluence

## Purpose

`collector-confluence` reads a bounded Confluence Cloud space or page tree and
emits source-neutral documentation facts. It is the first documentation-source
collector vertical slice for Eshu's read-only evidence boundary.

## Ownership Boundary

This binary owns process wiring only: Confluence config loading, read-only HTTP
client construction, `collector.Service` setup, Postgres ingestion, telemetry,
and the hosted status surface. It does not own documentation fact schema,
Confluence write behavior, or downstream documentation update workflows.
Section facts store the source-native Confluence storage body in Postgres for
later diff generation, but the collector still never calls Confluence mutation
APIs.

## Entry Points

- `main` and `run` in `go/cmd/collector-confluence/main.go`
- `buildCollectorService` in `go/cmd/collector-confluence/service.go`
- `go run ./cmd/collector-confluence` for local verification
- `eshu-collector-confluence --version` and `eshu-collector-confluence -v`
  print the build-time version before runtime setup begins

## Configuration

The collector requires a Confluence base URL, one bounded scope, read-only
credentials, and the standard Postgres env contract used by
`runtime.OpenPostgres`.

Required Confluence values:

- `ESHU_CONFLUENCE_BASE_URL`
- exactly one of `ESHU_CONFLUENCE_SPACE_ID` or `ESHU_CONFLUENCE_ROOT_PAGE_ID`
- either `ESHU_CONFLUENCE_BEARER_TOKEN` or both `ESHU_CONFLUENCE_EMAIL` and
  `ESHU_CONFLUENCE_API_TOKEN`

Optional values:

- `ESHU_CONFLUENCE_SPACE_KEY`
- `ESHU_CONFLUENCE_PAGE_LIMIT`
- `ESHU_CONFLUENCE_POLL_INTERVAL` (Go duration, defaults to `5m`)

## Telemetry

The binary uses the shared hosted runtime with `/healthz`, `/readyz`,
`/metrics`, and `/admin/status`. The Confluence source logs each completed
sync with `scope_id`, `page_count`, `failure_count`, and `freshness_hint`.
Shared collector metrics carry `collector_kind=documentation` and
`source_system=confluence`. Logs and metrics must not include page titles,
stored body content, or body excerpts.

## Invariants

- The collector is read-only. It only issues Confluence HTTP `GET` requests.
- It must emit documentation facts through `collector.Service`; do not write
  facts directly from the command package.
- Permission gaps in a page tree are partial-sync evidence, not fatal errors.
- Source metadata must preserve page count, failure count, and sync status so
  operators can distinguish complete and partial generations.

## Related Docs

- [Collector authoring](../../../docs/docs/guides/collector-authoring.md)
- [CLI reference](../../../docs/docs/reference/cli-reference.md)
- [Telemetry reference](../../../docs/docs/reference/telemetry/index.md)
