# Query

## Purpose

`internal/query` owns Eshu's HTTP read surface and the read models consumed by
API, MCP, and CLI workflows. It mounts `/api/v0` routes, assembles OpenAPI,
negotiates the canonical `{data, truth, error}` envelope, and gates
capabilities by runtime profile.

## Ownership boundary

Handlers read through ports such as `GraphQuery`, `ContentStore`, and
query-local read-store interfaces. Handler code must not import graph or SQL
drivers directly except in adapter/store files that explicitly own that seam.

OpenAPI fragments, public HTTP docs, truth-envelope fields, and MCP dispatch
must stay aligned whenever a public route or response shape changes.

## Exported surface

See `doc.go` and `go doc ./internal/query` for the contract. The stable anchors
are `APIRouter`, route handlers, envelope types, response writers, `GraphQuery`,
`ContentStore`, `OpenAPISpec`, runtime profiles, truth levels, and capability
matrices.

## Dependencies

`internal/status` supplies readiness/status data. `internal/telemetry` supplies
spans, DB instrumentation, and structured timing log keys. Truth-aligned
contracts are expressed through response envelopes and capability metadata.
Concrete storage adapters are wired by binaries; handlers depend on ports.

## Telemetry

Query handlers use spans and timing logs for expensive read families. Expensive
reads must be scoped, bounded, cancellable, ordered, and explicit about
truncation when lists can grow.

## Gotchas / invariants

- Envelope negotiation is public contract. Do not change MIME type or envelope
  fields without updating HTTP, MCP, docs, and tests together.
- Capability gates must fail explicitly for unsupported runtime profiles.
- Code-quality and dead-code responses preserve language maturity, exactness
  blockers, modeled roots, and source handles.
- Graph queries must be bounded by repository, workload, service, environment,
  or another canonical scope whenever possible.
- Entity-map reads resolve one typed start entity before traversal.
- Public route behavior, OpenAPI fragments, MCP dispatch, and public docs move
  together.

## Focused tests

```bash
cd go
go test ./internal/query -count=1
go test ./internal/mcp -count=1
go run ./cmd/eshu docs verify ../go/internal/query --limit 1200 \
  --fail-on contradicted,missing_evidence
```

## Related docs

- `docs/public/reference/http-api.md`
- `docs/public/reference/dead-code-reachability-spec.md`
- `docs/public/reference/truth-label-protocol.md`
- `docs/public/reference/capability-conformance-spec.md`
- `docs/public/reference/mcp-reference.md`
