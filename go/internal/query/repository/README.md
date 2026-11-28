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
`ServiceCatalogCorrelations`, `Freshness`, `Selections`, `Profile`, `Logger`, and
`Instruments` dependencies; `cmd/api` and `cmd/mcp-server` wire it through the root
`query.RepositoryHandler` alias unchanged. The OpenAPI fragments
documenting the repository routes live in `openapi/paths/repository/`
(the B2 precedent for family moves): the census co-move assumed room the
40-file cap does not have once the catalog method files and the semantic
trio the census missed are counted, so the fragments stay where the spec
assembly already references them.

## List selection annotation (#8454)

Every `GET /api/v0/repositories` entry carries an always-present `selection`
block (`selection_annotation.go`): the same `selection.Summarize` the
freshness read uses, rendered by the same block, from one batched live-rows
read for the page plus the bounded generation and successor statements only
when the page needs them. Both the content and graph paths annotate after
paging, keyed by the `repo_slug` both row shapes already carry; the MCP
`list_indexed_repositories` tool rides the same route. `Selections` is the
Postgres batch store in production and nil in tests that do not cover
annotation (entries read unknown). A failed annotation read fails the list;
`ResolveRepository` and the catalog workload rows never consult the store,
so their outcomes stay byte-identical with and without observation rows.
OpenAPI: the `Repository` component and the freshness `selection` object
carry the same nine keys.

## Context workload count

`getRepositoryContext` counts distinct graph `Workload` nodes reached through
`Repository` `DEFINES`. Repository context does not load the read-model
workload names (story and entity still do, for identity and display); names do
not establish materialization. A failed graph count aborts the
context response rather than reporting an inferred zero or name count. File,
platform, and dependency counts retain their existing read-model paths. The
Postgres dependency count reads only the active relationship generation: it
joins `relationship_generations` on `status = 'active'`, so rows left behind by
a superseded or pending generation do not count (#10824). Once the retirement
purge (#10820) lands, a deleted generation counts zero too; today a retired
repository's generation stays active, so it still counts.

Repository context uses the optional count-only Postgres read port for platform
and dependency counts. It does not load workload names. A missing or failed
count read falls back to the existing graph counts without retrying the full
summary. Legacy content stores can still use the full summary port. A workload
name read failure alone no longer changes context counts; story and entity
responses still use names. The count read runs before the `summary_counts`
stage starts, so that stage times only the count assembly (graph counts and any
read-model scalars already loaded) and does not see a Postgres count error or
the fallback. The read's timing and errors are on the bounded `postgres.query`
span with `db.operation=repository_context_counts`, which carries no repository
or scope identifier.

## Selector errors

Every `{repo_id}` route resolves its path selector through
`resolveRepositoryPathSelector` (stats through
`resolveRepositoryStatsPathSelector`). A graph-availability or reader-fence
verdict answers 503/504 through `querycontract.WriteGraphReadError`; any other
failed catalog or graph read answers 500 with the fixed `repository selector
lookup failed` body and an error on the request span
(`selector.WriteLookupFailure`, #7626); an unmatched selector answers 404 and
an ambiguous one answers 409 with the structured candidate body
(`writeSelectorAnswer` through `selector.WriteAmbiguousError`, #10655), and
any other error answers the same fixed 500 (#7674). The stats route answers 504 with the same fixed body
and span error when the selector read runs out its 2s route budget
(`context.DeadlineExceeded`). Coverage re-resolves the selector and answers
that second resolution the same way.

## Read failures

Every route step that fails a read maps a reader fence or graph verdict through
`querycontract.WriteGraphReadError` first, then answers through
`tracing.WriteServerFailure` with that step's fixed message from `failure.go`
and an error on the request span (`writeReadFailure`). One constant per step
(`repository story semantic overview read failed`, `repository tree file list
failed`, ...) lets an operator name the failing read from the body alone. A
read that failed because the caller canceled the request answers 499 with the
same body and only the `eshu.request.client_canceled` span event. The stats
repository lookup keeps 504 when its own route budget ran out. Before #7626
(stats and coverage) and #7674 (every other route) these answered with the
backend error text in the body.

Stats and story read content-store coverage and degrade a failed read to a 200
partial. `coverage.last_error` and the story's `coverage_summary.last_error`
are a closed set of fixed strings: the timeout text, `content store coverage
read failed`, `content store not configured`, or `content store coverage
unavailable`, never the error text. Before degrading, `writeCoverageVerdict`
answers a reader fence (503 with `Retry-After`) and a client cancel (499).

The fence rule (#7674 acceptance bullet 2): a fence answers the retryable 503
on every route whose read failure answers an error response, and no route folds
a fence into a null or fabricated headline fact; a route whose headline fact
falls back to a propagating, fenced read may attribute-degrade with a named
partial reason. Context is that case: its `file_count` falls back to a graph
count whose failure propagates through `writeReadFailure`, so a fence on its
content summary keeps the 200 with `repository_context_content_coverage_degraded`.
A client cancel of that read answers 499. Stats and story have no fallback (null
counts), so their fence answers 503.

## Story file list

`getRepositoryStory` reads the repository file list once, in the
`semantic_overview` stage (`loadRepositorySemanticOverview` returns it), and
shares that read-only slice with the infrastructure, deployment, narrative, and
CI/CD stages instead of listing the repository again per stage (#7126).

## Deployment-artifact bounds (#7951)

The story and context deployment-artifact stage runs
`repositoryartifacts.LoadDeploymentArtifactOverviewWithStats`. Its
`BoundReasons` go to story `limitations` (which then sets `truncated` and
`answer_metadata.truncated`) and context `partial_reasons`.
`DeploymentArtifactsReadCountAttrs` puts the read and candidate counts on the
`deployment_artifacts` and `content_infrastructure_overview` stage lines, and
`RecordDeploymentArtifactCandidates` adds the outcomes to
`eshu_dp_config_artifact_candidates_total` through the handler's
`Instruments` (nil records nothing). The service routes call both exported
helpers for their `deployment_evidence` stage.

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

The content coverage helper offers `QueryMaxIndexedAtWithRowQueryer` for a
guarded single-row connection. It shares the same closed table allowlist and
scan logic as the existing `QueryMaxIndexedAt(*sql.DB)` entry point.

## Exported surface

The exported surface is described in [doc.go](doc.go). Exports exist only
for staying callers: the other packages that consume repository reads
(`entity/workload_context.go`, `service/deployment_evidence.go`,
`deployment_trace_support_helpers.go`, documentation and target-support
stayers), the `cmd` wiring alias, and the staying root tests that pin
family behavior. Unexported helpers stay unexported; cross-package test
pins go through `testutil/graph` (`FakeGraphReader`,
`FakeRepoGraphReader`), `testutil/content` (`FakePortContentStore`), and
`testutil` (`FakeScopedTokenResolver`), or `querycontract` (row-value
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
(#6786).

## API surface endpoint paging (#10477)

`QueryRepoAPISurfacePaged` pages the endpoint set at the Cypher read for the
dedicated list route: the same count query reports the true total while the
detail read carries `SKIP $offset LIMIT $limit` beside the existing
`ORDER BY path, endpoint_id`, so page N never re-reads or Go-slices an
earlier page. The `SKIP`/`LIMIT` order is the repository-list pagination
shape proven on the pinned Neo4j backend (see `handler.go`); `OFFSET` is not
used. The unpaged `QueryRepoAPISurface` keeps its query text
byte-identical (cassette and snapshot stability); the paged variant shares
only the row mapping. The handler clamps pages into `[1, 100]` (default 50);
an empty set -- or an offset past the end -- answers 200 with the true
total, not 404.

## Dependencies

The package imports the Go standard library, `querycontract` (types, ports,
envelopes, shared bounds), `selector` (selector resolution),
`auth` (scoped-context checks), `impact`/`deployment` (deployment
seams), `repositoryartifacts`, `repository/readmodel`, `testutil` in
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
