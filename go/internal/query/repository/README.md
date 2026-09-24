# Repository handler family

## Purpose

`repository` holds the Handler HTTP surface and every file that
declares one of its methods (Issue #6060, lane B): the repository
list/context/story/stats/coverage/tree/content/branches/freshness routes,
the `GET /api/v0/catalog` routes and catalog workload-enrichment reads
served through the same handler, the entity-semantics shaping the story
reads share with the entity layer, and the deployment/config/workflow
evidence shaping behind the story responses. The `Handler` struct
keeps its `Neo4j`, `Content`, `CICDRunCorrelations`,
`ServiceCatalogCorrelations`, `Freshness`, `Profile`, and `Logger`
dependencies; `cmd/api` and `cmd/mcp-server` wire it through the root
`query.RepositoryHandler` alias unchanged. The OpenAPI fragments
documenting the repository routes live in `openapi/paths/repository/`
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
for staying callers: the other packages that consume repository reads
(`entity/workload_context.go`, `service/deployment_evidence.go`,
`deployment_trace_support_helpers.go`, documentation and target-support
stayers), the `cmd` wiring alias, and the staying root tests that pin
family behavior. Unexported helpers stay unexported; cross-package test
pins go through `querytestutil/graph` (`FakeGraphReader`,
`FakeRepoGraphReader`), `querytestutil/content` (`FakePortContentStore`), and
`querytestutil` (`FakeScopedTokenResolver`), or `querycontract` (row-value
decoders, shared bounds, ports).

## Dependency-edge reads

`loadRepositoryDependencyEdges` is the one graph read behind repository
dependency clusters and the `is_dependency` marker, for both
`GET /api/v0/repositories` and `GET /api/v0/catalog`. Unscoped callers run
the whole-graph `DEPENDS_ON` count first and skip the read when it is zero,
then run `RepositoryDependencyGroupedEdgeCypher` (`RETURN s.id,
collect(t.id)`), which NornicDB v1.3.3 answers from the relationship-type
index. Its `$group_limit` bounds source groups, not edges, so when the count
exceeds 50,000 or is unreadable the loader first runs
`RepositoryDependencyGroupSizeCypher` (`RETURN s.id, count(t)`) and passes the
smallest group prefix holding the first 50,000 edges. That caps the transfer
at 50,000 edges plus one source's edges (#6786 review R3-F2). Scoped callers run the grant-predicated per-edge read
(`repositoryDependencyClusterEdgeCypher`), because a `WHERE` clause disables
that fast path. Both paths return edges sorted by (source, target) and
clipped to 50,000, and report truncation. `dependency_edge_unscoped.go` holds
the unscoped probe, grouped read and loader; `dependency_edge_cap.go` holds the
group-size read and the prefix computation. Measurements are in
`docs/internal/evidence/6786-repository-dependency-marker-and-relationship-repo-anchor.md`.

Performance Evidence (#6786 review R2-F6): on NornicDB v1.3.3 with 500
repositories of 200 files each, the grouped read costs 0.0045s median against
0.635s for the per-edge read, with identical row sets at LIMIT 50001, 100 and
7 on NornicDB and Neo4j. `listCatalogRepositoriesFromGraph` went from
0.60-0.66s (per-edge) to 0.013s.

Performance Evidence (#6786 review R3-F2, recorded in
`docs/internal/evidence/6786-repository-dependency-edge-transfer-cap.md`): on
the same graph with 75,000 and 150,000 Repository `DEPENDS_ON` edges, the
capped read transfers 50,100 edges instead of all of them. The interleaved
loader median moves 0.160s to 0.169s and 0.154s to 0.166s on NornicDB, and
0.171s to 0.138s and 0.189s to 0.100s on Neo4j. Below the bound the statements are unchanged apart from the
parameterized LIMIT (NornicDB at 40,000 edges: 0.0423s to 0.0410s).

Observability Evidence (#6786 review R2-F10, R3-F2): both routes time the read
as `repository_query.stage_*` with `stage=dependency_cluster_edges`
(`operation=repository_list` or `catalog_list`), carrying `edge_count`,
`truncated`, `error`, `edge_scan_skipped` and `edge_transfer_capped`; only the
list route adds `cluster_count`.

## Dependencies

The package imports the Go standard library, `querycontract` (types, ports,
envelopes, shared bounds), `selector` (selector resolution),
`auth` (scoped-context checks), `impact`/`deployment` (deployment
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

No-Regression Evidence (#6642 rule 4): the export destutter renamed
fifteen identifiers and nothing else. Every SQL and Cypher literal in this
package is byte-identical to the pre-rename commit; the eight queryplan
source digests that moved did so only because the receiver type in the
recorded function text changed, and `go test ./internal/queryplan/` is
green on the re-pinned rows. `go test ./internal/query/... -count=1`,
`./cmd/api ./cmd/mcp-server ./internal/mcp`, and the parser-relationship-kit
verifier pass on the renamed tree; no benchmark delta is claimed because no
query changed shape.

No-Observability-Change (#6642 rule 4): the rename touches no span, metric,
or log name; `repository_query.stage_*` events and the tracer scope are
unchanged.

No-Observability-Change (#6060 lane-B B3): no new runtime behavior, so no
new spans, metrics, or logs. The existing `repository_query.stage_*`
telemetry events moved with their handlers unchanged; operator signals are
identical to base.
