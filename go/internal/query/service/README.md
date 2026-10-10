# Service handler family

## Purpose

`service` holds the service-handler family (Issue #6060, lane B): the
`CatalogHandler` HTTP surface (`GET
/api/v0/service-catalog/correlations` plus the local-descriptor evidence
reads behind it), every pure helper behind the service context, story,
investigation, evidence, hostname, ingress-posture, and deployment-evidence
reads, the service query enrichment with its row shaping, the service-story
dossier/overview/evidence-graph/supply-chain-scope/trace-path shaping, and
the family's `service_catalog.correlations.list` capability row. The OpenAPI
fragments documenting the service routes live in `openapi/paths/service/`,
which scripts/verify-openapi.sh reaches by scanning that tree recursively.

The `CatalogHandler` struct keeps its `Content`, `Correlations`,
and `Profile` dependencies; `cmd/api` and `cmd/mcp-server` wire it through
the root `query.ServiceCatalogHandler` alias unchanged.

## Ownership boundary

This package owns handler orchestration for the service routes and the pure
shaping behind the service reads. The `*Handler` investigation/story/
workload-resolution methods live in the `entity` package, and the
`*ContentReader` target-support methods stay in the query root: Go requires
methods to live with their receiver type. Those callers use this package's
exported homes (`service_alias.go` keeps every other caller compiling
unchanged).

The deployment-trace enrichment the service enrichment consumes
(provisioning candidates, source chains, consumer enrichment, hostname
bounds) lives in `deployment`, not here: it is deployment family, shared
with the staying deployment-trace wrappers. Shared read-model loaders, row
decoders, bounds, and ports live in `querycontract`; YAML/OpenAPI content
parsing lives in `service/evidence`; repository overviews live in
`repository`/`repositoryartifacts`. This package imports those leaves,
never the reverse, and never the query root (the root cycles back through
`handler.go` and `service_alias.go`).

The service capability (`service_catalog.correlations.list`) is declared by
this family in `capabilities.go` and registered through `querycontract`
like every other moved family; the root matrix no longer repeats it.

## Exported surface

`NewPostgresServiceCatalogCorrelationStoreWithReadStore` accepts a guarded,
query-only reader for service catalog correlations. The existing `*sql.DB`
constructor remains available for callers that have not been rewired.

Exports exist only for staying callers: the root stayers that consume
service reads (entity handlers, the service seam, the container-image
explanation, compare, the deployment-trace wrappers), the `cmd` wiring
aliases, the `serviceintelhttp` composer, and the staying root tests that
pin family behavior. Unexported helpers stay unexported; cross-package test
pins go through `testutil` or `querycontract`.

## Target-support gate

`story_target_support_reads.go` loads the story's `target_support` block
(#7138). A service story's repository-linked Jira support belongs to the
service only through the graph's `Repository-[:DEFINES]->Workload` edge, so the
loader runs one bounded read per story (`repositoryDefinesWorkloadsCypher`,
anchored on `Repository.id`, target sorted first with
`ORDER BY CASE WHEN id = $workload_id THEN 0 ELSE 1 END, id LIMIT 3`, no
aggregate) and hands the
verdict to the content store as `RepositoryWorkloadCount` and
`RepositoryDefinesTarget` on the filter. One defined workload equal to the
target links; several including the target make the rows ambiguous; every other
case fails closed, including no graph, a failed read (reported on the stage
event, never failing the story), and an identity-only context. The Postgres row
read and the ambiguity rule live in the query root's
`service_story_target_support.go` because they are `*ContentReader` methods.

## Dependencies

The package imports the Go standard library, `querycontract` (types, ports,
envelopes, shared bounds), `selector` (selector resolution),
`testutil`-adjacent fakes in tests only, `impact`/`deployment`
(deployment seams), `repository`/`repositoryartifacts` (deployment and
relationship overviews), `service/evidence` (spec parsing), `supplychain`
(image/SBOM read models), `doctruth` (image-ref normalization), and the
`telemetry`/`log` packages for the `service_query.stage_*` events. It never
imports the query root or graph drivers.

## Verification

Run focused `service` tests, then root `query`, `queryplan`, `mcp`,
`cmd/api`, and `cmd/mcp-server` suites, plus whole-module build and vet.
Run `scripts/verify-package-docs.sh` whenever this package changes. The B-7
cassettes and B-12 snapshot must stay byte-identical: this family moves
code, never Cypher text or queue/projection behavior. The
`query-source-coverage.yaml` file keys move with the functions; digests
change only when a function source actually changes, proven by the queryplan
test.

No-Regression Evidence (#6060 lane-B B4): this package is a pure move of
the service family from the query root (base 73ed3ad73) with no handler
logic changes — function bodies are identical modulo package qualifiers and
the documented export renames. Emitted Cypher text is byte-identical, pinned
by the queryplan production-binding tests (green) and the per-symbol
source_sha256 audits in `query-source-coverage.yaml` (2 legacy non_hot rows
converted to typed audits, digests verified).

No-Regression Evidence (#6642 rule 4): the export destutter renamed 43
identifiers and nothing else. Every SQL and Cypher literal in this package is
byte-identical to the pre-rename commit; the one queryplan digest that moved
with the rename (`buildServiceDocumentationOverview`) did so only because
the recorded function text names a renamed identifier, and
`go test ./internal/queryplan/` is green on the re-pinned rows.
`go test ./internal/query/... -count=1` and
`./cmd/api ./cmd/mcp-server ./internal/mcp` pass on the renamed tree,
including the route-serves-data anti-poison suite; no benchmark delta is
claimed because no query changed shape.

No-Observability-Change (#6642 rule 4): the rename touches no span, metric,
or log name; `service_query.stage_*` events, `QueryStageTimer`'s output and
the tracer scope are unchanged.

No-Observability-Change (#6060 lane-B B4): no new runtime behavior, so no
new spans, metrics, or logs. The existing `service_query.stage_*`
telemetry events moved with their handlers unchanged; operator signals are
identical to base.

## Failed reads (#7674)

A failed store read on `GET /api/v0/service-catalog/correlations` answers
the fixed `list service catalog correlations failed`, never the backend
error text. `querycontract.WriteGraphReadError` runs first (a stale or
timed-out reader answers the retryable 503 with `Retry-After`), then
`tracing.WriteServerFailure`: `500` with the error on the handler span, or
`499` when the caller canceled the request. The handler span now starts from
the package-local `catalogHandlerTracer` seam, seeded from
`tracing.HandlerTracer()`, so the failure test can swap in a recorder.

No-Regression Evidence (#7674): the change runs only after a read has already
returned an error. No SQL, query parameter, call count, row bound, or success
path changed. A failure now costs one span `RecordError`/`SetStatus` and a
fixed-string write instead of formatting the error into the body.
`go test ./internal/query/... ./internal/queryplan/... -count=1` and
`go test -race ./internal/query/service/...` exit 0.

Observability Evidence (#7674): a server fault records the backend error on
the handler span as an `exception` event and sets status Error with the fixed
message; a client cancel adds `eshu.request.client_canceled`, leaves the
status Unset, and answers `499`. The span name and attributes are unchanged.
`server_failure_test.go` asserts both span shapes with a recording tracer.
