# Entity handler family

## Purpose

`entity` holds the entity-handler family (Issue #6060, lane B B5): the
`Handler` HTTP surface (`POST /api/v0/entities/resolve`, `GET
/api/v0/entities/{id}/context`, `GET /api/v0/workloads/{id}/context|story`,
`GET /api/v0/services/{name}/context|story`, `GET
/api/v0/investigations/services/{name}`), every method-home file behind
entity resolution (identity, page, results, workload, content-type, metadata,
summary, context-content shaping), the workload context core
(`fetchWorkloadContext`/`fetchServiceWorkloadContext` with the read-model
fallback) plus the runtime-topology, provisioned-platform, and platform
reads behind it, and the absorbed B4 `*Handler` service seam (story
envelope, supply-chain enrichment, investigation, workload resolution). The
`platform_impact.context_overview` capability strings and the
`TruthBasisHybrid` envelope basis travel with the family; the shared
capability matrix row stays in the query root. The OpenAPI fragments
documenting the entity routes live in `openapi/paths/search/entities.go`,
which scripts/verify-openapi.sh reaches by scanning that tree recursively.

The `Handler` struct keeps its `Neo4j`, `Content`,
`CICDRunCorrelations`, `ContainerImageIdentities`, `SBOMAttachments`,
`Profile`, `Logger`, and `Instruments` dependencies, respelled onto the
leaf ports (`querycontract`, `supplychain`); `cmd/api` and `cmd/mcp-server`
wire it through the root `query.EntityHandler` alias unchanged.

## Ownership boundary

This package owns handler orchestration for the entity routes and the pure
shaping behind the entity reads. The `*ContentReader` target-support seam
stays in the query root: Go requires methods to live with their receiver
type, and `ContentReader` is a later lane's family. Shared read-model
loaders, row decoders, bounds, ports, envelopes, and the authorization seam
live in `querycontract`; selector resolution lives in `queryselector`;
service tech fingerprints and repo infrastructure reads live in
`repository`; service query-stage timing and evidence shaping live in
`service`; image/SBOM read models live in `supplychain`. This package
imports those leaves, never the reverse, and never the query root (the root
cycles back through `handler.go` and `entity_alias.go`).

Workload-context fetching (`FetchWorkloadContextForOperation`,
`FetchServiceReadModelWorkloadContext`) is this family's production seam to
the deployment-trace wrapper: the staying root
`family_impact_trace_deployment.go` builds an `entity.Handler` and calls it,
which is why those two methods are exported. The remaining exports
(`FetchWorkloadDeploymentTopology`, `FetchProvisionedPlatformResult`,
`ProvisionedPlatformResult`, `FetchWorkloadRuntimeTopology`) exist only for
staying root tests that pin family behavior; new callers must prefer the
HTTP surface or `querytestutil` doubles.

NornicDB: `GetEntityContext` and `FetchWorkloadContextForOperation` no longer
render the multi-line scoped `WHERE` group that was unreliable on the pinned
NornicDB v1.3.3 image (#6786); the grant is decided in Go. The one Cypher-side
grant is the single-line SHAPE-A `querycontract.WorkloadScopePredicate` on the
scoped name read below, which Go still re-checks. See `querycontract`'s README for the detail and
`querycontract.WorkloadGrantAdmitted`.

A name-keyed workload lookup (`GET /services/{name}/context` and its MCP twin)
reads a bounded candidate set in `workload_lookup.go`: up to
`querycontract.WorkloadSelectorCandidateBound`+1 rows ordered by `w.id`, each
carrying `collect(DISTINCT dr.id)` for its DEFINES repositories. For a scoped
caller the read's `WHERE` line also carries `WorkloadScopePredicate`, so the
bound counts granted rows only (past the 128-term SHAPE-A cap it fails closed
and emits `eshu_dp_query_scope_grant_inline_capped_total` with reason
`workload_context_name`). Go re-checks the rows with `WorkloadGrantAdmitted` and returns the lowest admitted id, so two
workloads that share a name no longer depend on which row an unordered
`LIMIT 1` happened to return. A page over the bound returns
`querycontract.ErrWorkloadSelectorCandidatesExceedBound`, which the handler
writes as a count-free 409. An id-only lookup still reads one row, because
`Workload.id` is unique. `fetchServiceWorkloadContext` counts
`reason=grant_denied` once per request, and only when the name lookup, the id
lookup, and the read model all came back empty.

## Exported surface

Exports exist only for staying callers: the root deployment-trace wrapper,
the `cmd` wiring alias, and the staying root tests that pin family
behavior. Unexported helpers stay unexported; cross-package test pins go
through `querytestutil` (notably `FakePortContentStore` and
`FakeRepoGraphReader`) or `querycontract`.

## Dependencies

The package imports the Go standard library, `querycontract` (types, ports,
envelopes, shared bounds, authorization seam), `queryselector` (selector
resolution), `querytestutil`-adjacent fakes in tests only, `repository`
(tech fingerprint, repo dependency/infrastructure reads), `service` (query
timing, story/enrichment shaping), `supplychain` (image/SBOM read models),
and the `telemetry`/`log` packages for handler instruments. It never
imports the query root or graph drivers.

## Verification

Run focused `entity` tests, then root `query`, `queryplan`, `mcp`,
`cmd/api`, and `cmd/mcp-server` suites, plus whole-module build and vet.
Run `scripts/verify-package-docs.sh` whenever this package changes. The B-7
cassettes and B-12 snapshot must stay byte-identical: this family moves
code, never Cypher text or queue/projection behavior. The
`query-source-coverage.yaml` file keys move with the functions; digests
change only when a function source actually changes, proven by the queryplan
test.

No-Regression Evidence (#6060 lane-B B5): this package is a pure move of
the entity family from the query root (base a66064728) with no handler
logic changes — function bodies are identical modulo package qualifiers and
the documented export renames. Emitted Cypher text is byte-identical, pinned
by the queryplan production-binding tests (green) and the per-symbol
source_sha256 audits in `query-source-coverage.yaml`.

No-Observability-Change (#6060 lane-B B5): no new runtime behavior, so no
new spans, metrics, or logs. The existing entity telemetry events
(instruments, k8s-scan truncation disclosure) moved with their handlers
unchanged; operator signals are identical to base.

## Naming (#6642 Part D)

Destuttered under `docs/internal/naming.md` rules 2 and 4: file names no
longer repeat the leaf's own package word, and the one exported symbol that
did (`EntityHandler`) dropped the stutter. Behavior is unchanged; only file
names, identifier spellings, citations, and doc comments moved.

### Files (rule 2)

| Old | New |
| --- | --- |
| `entity.go` | `handler.go` |
| `entity_content_types.go` | `content_types.go` |
| `entity_content_types_atlantis_test.go` | `content_types_atlantis_test.go` |
| `entity_context_authz_test.go` | `context_authz_test.go` |
| `entity_context_content.go` | `context_content.go` |
| `entity_context_content_truncation_test.go` | `context_content_truncation_test.go` |
| `entity_context_limits.go` | `context_limits.go` |
| `entity_context_truth.go` | `context_truth.go` |
| `entity_helpers.go` | `helpers.go` |
| `entity_metadata.go` | `metadata.go` |
| `entity_repository_selector_test.go` | `repository_selector_test.go` |
| `entity_resolve_identity.go` | `resolve_identity.go` |
| `entity_resolve_page.go` | `resolve_page.go` |
| `entity_resolve_results.go` | `resolve_results.go` |
| `entity_resolve_results_test.go` | `resolve_results_test.go` |
| `entity_resolve_workload.go` | `resolve_workload.go` |
| `entity_resolve_workload_query.go` | `resolve_workload_query.go` |
| `entity_resolve_workload_test.go` | `resolve_workload_test.go` |
| `entity_story_kotlin_test.go` | `story_kotlin_test.go` |
| `entity_story_typescript_test.go` | `story_typescript_test.go` |
| `entity_summary.go` | `summary.go` |
| `entity_workload_context.go` | `workload_context.go` |
| `entity_workload_context_test.go` | `workload_context_read_model_test.go` (collision: `workload_context_test.go` already existed; renamed to name what it actually covers — the read-model fallback path — rather than folding into the existing file, which would have pushed it past the 500-line cap) |
| `entity_workload_handlers.go` | `workload_handlers.go` |
| `entity_workload_platform.go` | `workload_platform.go` |

### Exported identifier (rule 4)

| Old | New |
| --- | --- |
| `EntityHandler` | `Handler` |

The root alias keeps its pre-move spelling: `query.EntityHandler = entity.Handler`
(`entity_alias.go`, outside this leaf). `family_impact_trace_deployment.go`
(also outside this leaf) was repointed to construct `entity.Handler` directly.

### Unexported package-level identifiers leading with `entity` (also renamed)

| Old | New | File |
| --- | --- | --- |
| `entityResolveRank` | `resolveRank` | `resolve_results.go` |
| `entityHasStableIdentity` | `hasIdentity` | `resolve_results.go` |
| `entityIsAnonymousContainer` | `isAnonymousContainer` | `resolve_results.go` |
| `entityString` | `stringField` | `resolve_results.go` |
| `entityLabelStrings` | `labelStrings` | `resolve_results.go` |
| `entityResolveTruthEnvelope` | `resolveTruthEnvelope` | `resolve_page.go` |
| `entityContextTruthEnvelope` | `contextTruthEnvelope` | `context_truth.go` |
| `entityContextResultLimits` | `contextResultLimits` | `context_limits.go` |

`entityHasStableIdentity` could not become the equally-obvious `hasStableIdentity`:
`normalizeResolvedEntities` (`resolve_results.go`) already declares a local
`hasStableIdentity` bool in the same scope it calls this function from, so
that rename would have shadowed the call with the local variable. `hasIdentity`
avoids the collision while still dropping the package-word stutter.

### `EntityType` — naming rule 4 does not apply

`EntityType` is not a top-level export of this package. This package declares
no `type EntityType`, `func EntityType...`, `var EntityType`, or
`const EntityType` — only an ordinary struct field `EntityType string` on
`GlobalContentEntityFilter` (`content_types.go`), reached through a value
(`filter.EntityType`) and never as `entity.EntityType`. Naming rule 4 targets
a package-qualified export stuttering with the package name; a struct field
reached through its owning value is not that shape, so rule 4 does not apply
and nothing was renamed here.

The widely-cited `entity.EntityType` usage across
`go/internal/reducer/servicecatalog`, `go/internal/projector`,
`go/internal/collector/repo/git`, `go/internal/content/shape`,
`go/internal/storage/postgres`, `go/internal/searchpostgres`, and assorted
`go/internal/query/*` files (measured via
`rg -l '\bentity\.EntityType\b' --glob '!go/internal/query/entity/**' go`,
50 files at this head) is a textual coincidence, not a caller of this
package: none of those files import `go/internal/query/entity`, and every
hit is a local loop or parameter variable literally named `entity` (of some
other type, e.g. `querycontract.EntityContent` or `shape.Entity`) accessing
that other type's own `EntityType` field. Verified for a sample of the list
(`go/internal/reducer/servicecatalog/service_catalog_correlation_index.go`,
`go/internal/collector/repo/git/discovery_advisory.go`) by confirming no
`"github.com/eshu-hq/eshu/go/internal/query/entity"` import exists in those
files.

No-Regression Evidence (#6642 rule 2/4): `go test ./internal/query/... -count=1`,
`go test ./cmd/api ./cmd/mcp-server ./internal/mcp ./internal/queryplan/...
./cmd/golden-corpus-gate/... -count=1`, `go build ./...`, and `go vet ./...`
all exit 0 on the renamed tree; `go test ./internal/query/entity -list '.*'`
lists the identical sorted test-function set before and after (93 names).
The queryplan digests that moved did so only because the recorded function
text names the renamed `Handler` receiver, not because any Cypher, row
shape, or bound changed; `go test ./internal/queryplan/... -count=1` is
green on the re-pinned rows. The B-7 cassettes and B-12 golden snapshot are
byte-identical (`git diff origin/main --stat -- testdata/golden
testdata/cassettes` is empty).

Four citations of the old names are left in place on purpose, with the
reason recorded here rather than silently: `docs/internal/design/5385-workload-identity-key.md`
row 692 cites root-qualified `go/internal/query/entity.go` (line 283), which was
already dead before this PR, in a table row whose bytes anchor another
citation's LINE-ledger authority (repointing it would invalidate that
authority); and comments in `go/internal/query/language_query_graph_error_test.go`
(bare `entity_content_types.go`), `go/internal/query/language/metadata.go`
and `go/internal/query/querycontract/language_query_metadata.go` (bare
`entity_metadata.go`) keep the old names, because any edit to a `*language*`
file trips `scripts/verify-parser-relationship-kit.sh`, which then demands
an unrelated Language Query DSL doc update. All become fixable when their
respective gates are widened; none is a live path the gates resolve.

No-Observability-Change (#6642 rule 2/4): file and identifier renames only;
no span, metric, log, status field, or route changes, so the telemetry
contract this package documents above is untouched.
