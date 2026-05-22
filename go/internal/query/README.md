# Query

## Purpose

`internal/query` owns Eshu's HTTP read surface and the read models consumed by
API, MCP, and CLI query workflows. It mounts `/api/v0` routes, assembles the
static OpenAPI document, negotiates the `{data, truth, error}` envelope, and
gates capabilities by runtime profile.

## Ownership boundary

Query handlers read through ports such as `GraphQuery`, `ContentStore`, and
query-local read-store interfaces. Handler code must not import Neo4j,
NornicDB, or `*sql.DB` directly. Backend-specific behavior belongs behind the
ports or in documented adapter seams.

Public route flow:

```text
validate request
  -> check capability/profile gate
  -> read through GraphQuery, ContentStore, or read-store port
  -> return deterministic bounded result
  -> WriteSuccess / response envelope
```

## Exported surface

See `doc.go` and `go doc ./internal/query` for the full godoc contract. The
main package contracts are:

- `APIRouter` and route-family handlers such as repository, entity, code,
  content, infrastructure, IaC, impact, evidence, documentation, package
  registry, CI/CD, supply-chain, status, compare, and admin handlers.
- `ResponseEnvelope`, `TruthEnvelope`, `BuildTruthEnvelope`,
  `EnvelopeMIMEType`, `WriteSuccess`, `WriteJSON`, `WriteError`, and
  `WriteContractError`.
- `GraphQuery`, `ContentStore`, and query-local read-store interfaces.
- `OpenAPISpec` and `openapi_paths_*.go` string fragments.
- `QueryProfile`, `TruthLevel`, `TruthBasis`, and the capability matrix in
  `contract.go`.

## Dependencies

- `internal/status` for status/readiness route data.
- `internal/telemetry` for query spans, DB query instrumentation, and structured
  timing log keys.
- `internal/truth`-aligned contracts through query truth envelopes and
  capability metadata.
- Storage adapters are wired by binaries; handlers should depend on ports.

## Telemetry

Distinct user-visible query families use spans such as
`query.relationship_evidence`, `query.evidence_citation_packet`,
`query.documentation_findings`, `query.documentation_facts`,
`query.documentation_evidence_packet`, `query.documentation_packet_freshness`,
`query.code_topic_investigation`, `query.hardcoded_secret_investigation`,
`query.dead_code_investigation`, `query.change_surface_investigation`,
`query.resource_investigation`, `query.dead_iac`,
`query.iac_unmanaged_resources`, `query.iac_management_status`,
`query.iac_management_explanation`, `query.iac_terraform_import_plan`,
`query.aws_runtime_drift_findings`, and `query.infra_resource_search`.

Storage reads are visible through `neo4j.query`, `postgres.query`,
`eshu_dp_neo4j_query_duration_seconds`, and
`eshu_dp_postgres_query_duration_seconds`. Repository and service story paths
also emit stage timing logs.

## Gotchas / invariants

- `BuildTruthEnvelope` panics for a capability missing from the capability
  matrix. Add the matrix entry before wiring a handler that returns truth.
- Envelope-aware callers only receive `ResponseEnvelope` when they send
  `Accept: application/eshu.envelope+json`.
- `AuthMiddleware` skips auth for public paths and when no token is resolved.
  Do not add data routes to public paths without explicit review.
- OpenAPI fragments are static strings; route or response changes must update
  the matching fragment and public HTTP docs in the same PR.
- List-style routes need scope, limit, deterministic ordering, and truncation
  metadata. Whole-graph reads need explicit product justification.
- Entity-map reads resolve exactly one typed start entity before traversal.
  Repository anchors use direct relationship-family traversal by default so
  structural edges do not expand before `limit` can bound the result. Keep
  `TestEntityMap` coverage with route or query-shape changes.
- Package-registry reads stay anchored by package, version, ecosystem, or
  repository scope; source hints are not ownership truth. Keep package-list
  Cypher on direct scalar aliases instead of an intermediate `WITH p, count(v)`,
  guarded by `TestPackageRegistryListPackagesUsesIndexedPackageScopeAndTruncates`.
- Dead-code responses preserve language maturity, modeled roots, exactness
  blockers, and source handles. Do not duplicate the language root model in
  this README; use the dead-code root files and public spec.
- Content misses can mean the ingester has not written content yet; do not turn
  every `source_backend=unavailable` response into a Postgres error.

## Verification

Use the smallest command that proves the changed contract:

```bash
cd go
go test ./internal/query -count=1
go test ./internal/mcp -count=1
go doc ./internal/query
go run ./cmd/eshu docs verify ../go/internal/query --limit 1000 \
  --fail-on contradicted,missing_evidence
```

Route or response-shape changes also need the matching OpenAPI, public HTTP
reference, MCP, and capability-matrix tests.

## Related docs

- `docs/public/reference/http-api.md`
- `docs/public/reference/dead-code-reachability-spec.md`
- `docs/public/reference/truth-label-protocol.md`
- `docs/public/reference/capability-conformance-spec.md`
- `specs/capability-matrix.v1.yaml`
