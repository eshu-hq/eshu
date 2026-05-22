# collector-confluence

## Purpose

`collector-confluence` reads a bounded Confluence Cloud space, an explicit list
of spaces, or a page tree and emits source-neutral documentation facts. It is
the process wrapper for the read-only Confluence collector.

## Ownership boundary

This binary owns process wiring only: Confluence config loading, read-only HTTP
client construction, `collector.Service` setup, Postgres ingestion, telemetry,
and the hosted status surface. It does not own documentation fact schema,
Confluence write behavior, or downstream documentation update workflows.
Section facts store the source-native Confluence storage body in Postgres for
later diff generation, but the collector still never calls Confluence mutation
APIs.

## Exported surface

This is a `package main` binary. Its public contract is the process entrypoint,
`--version` / `-v`, bounded Confluence source configuration, shared Postgres
runtime env, and the hosted `/healthz`, `/readyz`, `/metrics`, and
`/admin/status` surface.

## Dependencies

- `internal/collector` for service orchestration and durable fact commits.
- `internal/collector/confluence` for config, HTTP client, source, and emitted
  documentation facts.
- `internal/app`, `internal/runtime`, `internal/storage/postgres`, and
  `internal/telemetry` for process hosting, persistence, status, and signals.

## Telemetry

The binary uses the shared hosted runtime. Confluence source metrics and the
shared collector metrics show HTTP request count/duration, permission gaps,
document/section/link emission, sync failure class, emitted fact count, and
committed fact count. Logs include `scope_id`, page count, failure count, and
freshness hint, but must not include page titles, body content, or excerpts.

## Gotchas / invariants

- The collector is read-only. Source code only issues Confluence HTTP `GET`
  requests.
- It must emit documentation facts through `collector.Service`; do not write
  facts directly from the command package.
- Multi-space mode must stay an explicit allowlist. Do not turn a blank
  `ESHU_CONFLUENCE_SPACE_IDS` value into site-wide crawling.
- Permission gaps in a page tree are partial-sync evidence, not fatal errors.
- Source metadata must preserve page count, failure count, and sync status so
  operators can distinguish complete and partial generations.

## Focused tests

```bash
cd go
go test ./cmd/collector-confluence ./internal/collector/confluence -count=1
```

## Related docs

- `go/internal/collector/confluence/README.md`
- `docs/public/guides/collector-authoring.md`
- `docs/public/reference/telemetry/index.md`
