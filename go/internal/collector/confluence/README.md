# Collector Confluence

## Purpose

`internal/collector/confluence` reads Confluence Cloud documentation evidence and
emits Eshu documentation facts. The package is read-only. It never writes back to
Confluence and it does not infer source truth without caller-provided extraction
hints.

## Ownership boundary

The package owns Confluence config parsing, read-only HTTP access, page
normalization, storage-body link extraction, optional documentation-truth fact
emission, and `collector.ObservedSource` observations. The command package owns
process wiring and Postgres commits; downstream documentation services own
diffs, excerpts, and publication workflows.

## Exported surface

See `doc.go` and `go doc ./internal/collector/confluence` for the godoc
contract. Callers use `Source`, `LoadConfig`, `NewHTTPClient`, `Client`, and
the Confluence page/config value types. `ErrPermissionDenied` marks 403/404
page gaps that can become partial-sync evidence.

## Dependencies

- `internal/collector` for source and observation contracts.
- `internal/doctruth` when callers enable mention and claim-candidate facts.
- `internal/facts`, `internal/scope`, and `internal/telemetry` for fact,
  generation, and metric contracts.

## Telemetry

`Source.NextObserved` starts the shared `collector.observe` span only after a
non-drained poll. Confluence metrics cover bounded HTTP requests and durations,
permission-denied pages, emitted documents/sections/links, sync failures, and
shared fact emission/commit counts. Metric labels must stay bounded to
operation, result, status class, failure class, source system, collector kind,
and scope ID.

## Gotchas / invariants

- Exactly one bounded scope mode is required: one space, an explicit space-ID
  allowlist, or one root page tree. Blank multi-space config must not crawl a
  whole Confluence site.
- `Source.Next` emits one bounded generation. In multi-space mode each call
  emits one configured space, and the drained poll resets the cycle.
- Non-current pages are skipped, duplicate page IDs collapse to the latest
  visible revision, and empty spaces are valid.
- 403 and 404 responses map to `ErrPermissionDenied`.
- Permission gaps in a page tree are partial-sync evidence; other client
  failures fail the generation because the collector cannot prove source state.
- Page IDs, titles, URLs, paths, body content, and excerpts do not belong in
  metric labels.

## Focused tests

Run focused checks after changing this package:

```bash
cd go
go test ./internal/collector/confluence ./cmd/collector-confluence -count=1
go run ./cmd/eshu docs verify ../go/internal/collector/confluence --limit 1000 \
  --fail-on contradicted,missing_evidence
```

## Related docs

- `go/cmd/collector-confluence/README.md`
- `go/internal/collector/README.md`
- `docs/public/guides/collector-authoring.md`
