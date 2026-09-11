# Repository handler family

## Purpose

`repository` holds the RepositoryHandler HTTP surface and every file that
declares one of its methods (Issue #6060, lane B): the repository
list/context/story/stats/coverage/tree/content/branches/freshness routes,
the `GET /api/v0/catalog` routes and catalog workload-enrichment reads
served through the same handler, the entity-semantics shaping the story
reads share with the entity layer, and the deployment/config/workflow
evidence shaping behind the story responses. The `RepositoryHandler` struct
keeps its `Neo4j`, `Content`, `CICDRunCorrelations`,
`ServiceCatalogCorrelations`, `Freshness`, `Profile`, and `Logger`
dependencies; `cmd/api` and `cmd/mcp-server` wire it through the root
`query.RepositoryHandler` alias unchanged. The OpenAPI fragments
documenting the repository routes stay in the query root beside `openapi.go`
(the B2 precedent for family moves): the census co-move assumed room the
40-file cap does not have once the catalog method files and the semantic
trio the census missed are counted, so the fragments stay where the spec
assembly already references them.

## Ownership boundary

This package owns handler orchestration for the repository routes. Story,
context, and evidence shaping the family needs but that reads file content
lives in `repositoryartifacts`; ref resolution and page shaping live in
`repository/readmodel`; shared read-model loaders live in `querycontract`.
This package imports `repositoryartifacts` and `repository/readmodel`, never
the reverse, and no package here imports the query root (the root cycles
back through root `handler.go` and `repository_alias.go`).

The staying root package keeps thin aliases and forwarders
(`repository_alias.go`, `repository_compat.go`) plus the ContentReader
read-model files, which cannot move: Go requires methods on
`*ContentReader` to stay in the package that defines the type. The
repository capabilities (`platform_impact.context_overview`,
`platform_impact.catalog`) are shared with the service, workload, entity,
and catalog stayers, so their rows stay in the root matrix; this package
gates through `querycontract` like every other caller.

## Exported surface

The exported surface is described in [doc.go](doc.go). Exports exist only
for staying callers: the root stayers that consume repository reads
(`entity_workload_context.go`, `service_deployment_evidence.go`,
`deployment_trace_support_helpers.go`, documentation and target-support
stayers), the `cmd` wiring alias, and the staying root tests that pin
family behavior. Unexported helpers stay unexported; cross-package test
pins go through `querytestutil` (`FakeGraphReader`,
`FakePortContentStore`, `FakeRepoGraphReader`, `FakeScopedTokenResolver`)
or `querycontract` (row-value decoders, shared bounds, ports).

## Dependencies

The package imports the Go standard library, `querycontract` (types, ports,
envelopes, shared bounds), `queryselector` (selector resolution),
`queryauth` (scoped-context checks), `impact`/`impacttrace` (deployment
seams), `repositoryartifacts`, `repository/readmodel`, `querytestutil` in
tests only, and the `telemetry`/`log` packages for the
`repository_query.stage_*` events. It never imports the query root or graph
drivers.

## Verification

Run focused `repository` tests, then root `query`, `queryplan`, `mcp`,
`cmd/api`, and `cmd/mcp-server` suites, plus whole-module build and vet.
Run `scripts/verify-package-docs.sh` whenever this package changes. The B-7
cassettes and B-12 snapshot must stay byte-identical: this family moves
code, never Cypher text or queue/projection behavior. The
`query-source-coverage.yaml` file keys and `grandfathered_non_hot.go`
entries move with the functions; bounds and digests change only when a
function source actually changes, proven by the queryplan test.

No-Regression Evidence (#6060 lane-B B3): this package is a pure move of
the RepositoryHandler family from the query root (base 2d8258ecc) with no
handler logic changes — function bodies are identical modulo package
qualifiers and export renames. Emitted Cypher text is byte-identical, pinned
by `queryplan_legacy_production_binding_test.go` (green) and the per-symbol
source_sha256 audits in `query-source-coverage.yaml` (13 legacy rows
converted to typed audits, digests verified). After measurement on this
commit: `go build ./...` clean; `go test ./internal/query/...` 17 packages
ok, 0 failures; `./internal/mcp/...` ok; `./internal/queryplan/...` ok;
`go vet` clean on all three trees; `git diff --check` clean;
`verify-dirgate.sh --all` exit 0. No benchmark delta is claimed because no
hot path changed shape; the suites above are the no-regression proof.

No-Observability-Change (#6060 lane-B B3): no new runtime behavior, so no
new spans, metrics, or logs. The existing `repository_query.stage_*`
telemetry events moved with their handlers unchanged; operator signals are
identical to base.
