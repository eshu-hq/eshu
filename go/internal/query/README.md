# Query

## Purpose

`internal/query` owns Eshu's HTTP read surface and the read models consumed by
API, MCP, and CLI workflows. It mounts `/api/v0` routes, assembles OpenAPI,
negotiates the canonical `{data, truth, error}` envelope, and gates
capabilities by runtime profile.

## Ownership boundary

Handlers read through ports such as `GraphQuery`, `ContentStore`, and
query-local read-store interfaces. Handler code must not import Neo4j,
NornicDB, or `*sql.DB` directly except in adapter/store files that explicitly
own that seam.

OpenAPI fragments, public HTTP docs, truth-envelope fields, and MCP dispatch
must stay aligned whenever a public route or response shape changes.

## Exported surface

Use `go doc ./internal/query` for the full godoc contract. The main contracts
are:

- `APIRouter` and route-family handlers for repositories, entities, code,
  content, infrastructure, IaC, impact, evidence, documentation, package
  registry, CI/CD, supply chain, status, compare, and admin.
- `ResponseEnvelope`, `TruthEnvelope`, `BuildTruthEnvelope`,
  `EnvelopeMIMEType`, `WriteSuccess`, `WriteJSON`, `WriteError`, and
  `WriteContractError`.
- `GraphQuery`, `ContentStore`, and query-local read-store interfaces.
- `OpenAPISpec` and `openapi_paths_*.go` fragments.
- `QueryProfile`, `TruthLevel`, `TruthBasis`, and capability matrices.

## Dependencies

- `internal/status` supplies status/readiness data.
- `internal/telemetry` supplies spans, DB instrumentation, and structured
  timing log keys.
- `internal/truth`-aligned contracts are expressed through truth envelopes and
  capability metadata.
- Concrete storage adapters are wired by binaries; handlers depend on ports.

## Telemetry

Query handlers use spans for visible read families such as relationship
evidence, citation packets, documentation findings/facts, workload/service
context, deployment trace, dead code, call graph metrics, package registry,
container image identity, and supply-chain reads. Keep expensive reads scoped,
bounded, cancellable, and ordered with a truncation signal where lists can grow.

## Gotchas / invariants

- Envelope negotiation is public contract. Do not change MIME type or envelope
  fields without updating HTTP, MCP, and tests together.
- Capability gates must fail explicitly for unsupported runtime profiles.
- Code-quality and dead-code responses must preserve language maturity,
  exactness blockers, modeled roots, and source handles so callers can
  distinguish actionable findings from ambiguous evidence.
- Graph queries must be bounded by repository, workload, service, environment,
  or another canonical scope whenever possible.
- Entity-map reads must resolve one typed start entity before traversal.
  Repository anchors use direct relationship-family traversal by default so
  structural edges do not expand before `limit` can bound the result.
- Public route behavior, OpenAPI fragments, MCP dispatch, and public docs must
  move together.

## Focused tests

```bash
go test ./internal/query -count=1
go test ./internal/mcp -count=1
go doc ./internal/query
```

## Related docs

- `docs/public/reference/http-api.md`
- `docs/public/reference/dead-code-reachability-spec.md`
- `docs/public/reference/truth-label-protocol.md`
- `docs/public/reference/capability-conformance-spec.md`
- `docs/public/reference/mcp-reference.md`
