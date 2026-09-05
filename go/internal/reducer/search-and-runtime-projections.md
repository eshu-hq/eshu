# Reducer Search And Live-Runtime Projections

Split from `README.md` (issue #5786). Live-workload `RUNS_IMAGE` edge
projection and the curated `EshuSearchDocument` search read model
live here; keep the package overview in `README.md`.

## Live-workload RUNS_IMAGE edge projection (issue #388 PR3)

`DomainKubernetesCorrelationMaterialization`
(`kubernetes_correlation_materialization.go`) is the gated graph-write slice that
closes the #388 chain. It mirrors `DomainAWSRelationshipMaterialization` (#805
PR2) and `DomainObservabilityCoverageMaterialization` (#391 PR3):

- It gates on `GraphProjectionKeyspaceKubernetesWorkloadUID` /
  `canonical_nodes_committed` (published by
  `DomainKubernetesWorkloadMaterialization`) so an edge never resolves against a
  workload node that has not committed. The miss is a retryable error so the
  durable queue re-runs the intent rather than failing terminally or writing
  against absent nodes. The durable Postgres claim gate
  (`reducer_queue_claim_query.go`, `reducer_queue_batch.go`) and the blockage
  view (`status_blockage.go`) carry a matching `kubernetes_workload_uid` clause.
- `ExtractKubernetesCorrelationEdgeRows` re-runs the PR1 classifier and promotes
  to an edge **only** an `exact` image decision that resolved both a workload
  node uid (`object_id`) and a digest-addressed OCI source node uid (resolved via
  `SourceImageDigestJoinIndex.ResolveDigestNode`). Derived / ambiguous /
  unresolved / stale / rejected outcomes stay provenance-only; the structural
  `owner_reference` identity decision is a workload→workload edge whose owner
  target is not guaranteed to have a `KubernetesWorkload` node, so it carries no
  `SourceDigest` and is naturally excluded from this image-edge slice. An exact
  decision whose digest resolves no canonical node (tag-only evidence) is counted
  skipped, never written as a dangling edge.
- **CRI-resolved digest promotion (#5432)**: When a pod_template container
  declares a tag-form image ref (e.g. `nginx:1.25`) AND carries a CRI-resolved
  `resolved_image_digest` (from `pod.Status.ContainerStatuses[].ImageID`
  normalized to `repo@sha256:<digest>` form), the classifier routes through
  `classifyImageByCRIDigest` — parsing the resolved digest as a repository+digest
  pair and resolving against the source digest index via the DIGEST path, never
  falling through to the weaker tag classification. When the resolved digest
  matches an active deployment-source observation, the outcome is `exact` /
  `JoinMode=digest` / `ProvenanceOnly=false` (edge-eligible). When the resolved
  digest has NO source observation, the outcome is `unresolved` /
  `driftMissingSource` / provenance-only — the CRI digest is ground truth of what
  is running, and a missing source is unresolved, not tag-derived. Without a
  CRI-resolved digest (Deployments, ReplicaSets, pending pods), behavior stays
  byte-identical to today: tag-form refs fall through to `classifyImageByTag`
  which is always provenance-only `Derived` / `Ambiguous` / `Unresolved`.
  Two containers sharing a declared tag ref can legitimately resolve to
  different digests — a mutable tag that moves between pulls is the ordinary
  cause. The observation is not malformed; the reference simply no longer names
  one running image, so no single runtime identity can be promoted from it.
  `resolvedImageDigestsFromTemplate` keeps every distinct digest and the outcome
  is `ambiguous` / `driftUnknown` / provenance-only, with every candidate
  recorded in `CandidateSourceDigests` and an explicit `NonPromotion` (#5517).
  The earlier first-wins policy promoted whichever container was read first to
  `exact`, which asserted a specific running image that was wrong whenever the
  digests disagreed. Candidates are compared by content digest, not by the raw
  `repo@sha256:` string, so one digest reached through two registry or mirror
  spellings stays a single candidate and still promotes to `exact`.
- The write is idempotent on `(workload_uid, RUNS_IMAGE, source_uid)`; rows are
  deduplicated and sorted so retries and reprojections produce a byte-stable
  batch. The conflict key is per-edge, so no serialization workaround is
  introduced (this is not a "serialization is not a fix" case).

Performance Evidence: `go test ./internal/reducer -run '^$' -bench
'BenchmarkExtractKubernetesCorrelationEdgeRows' -benchmem -benchtime=100x`
resolved 5,000 workloads → 5,000 edges in `8.89 ms/op` (`22.4 MB/op`,
`135,221 allocs/op`) on darwin/arm64 (Apple M3 Pro): the pure classifier plus the
O(M) digest→uid index build and O(1) per-edge source resolution, no per-edge
graph round trip and no N+1.
No-Regression Evidence (#5517 conflict detection): the classifier now keeps every
distinct CRI-resolved digest per declared reference instead of the first, adding
one slice-membership check per container and, only when a reference reported more
than one digest, a sort of that reference's candidates (bounded by container
count). Measured on darwin/arm64 (Apple M3 Pro, 12 logical CPUs) with
`go test ./internal/reducer -run '^$' -bench
'BenchmarkExtractKubernetesCorrelationEdgeRows' -benchmem -benchtime=100x
-count=3`, base `c1245790b` vs this branch, same machine and same fixture:
`12.95/13.02/12.99 ms/op` before and `13.11/13.06/13.03 ms/op` after, with an
identical allocation profile (`210,223 allocs/op`, `28.5 MB/op` both). That is
roughly +0.5% median against a run-to-run spread of about 0.6%, so the cost sits
at the edge of this benchmark's noise rather than clearly above it. The fixture
contains no conflicting pair, so this measures the ordinary agreeing path — the
path every workload takes.
No-Observability-Change: the conflict outcome reuses the existing correlation
decision payload (`candidate_source_digests`, `non_promotion`, `drift_kind`)
already published by `kubernetes_correlation_writer.go` and already read by
`internal/query`; no new metric, span, or log is introduced.
No-Regression Evidence: the edge write reuses the established UNWIND-batched
MATCH-MATCH-MERGE shape; `BenchmarkKubernetesCorrelationEdgeWriter` (in
`go/internal/storage/cypher`) shaped 5,000 edges at batch 500 in `1.14 ms/op`
(`2.16 MB/op`, `25,098 allocs/op`), faster and leaner than the proven
`BenchmarkCloudResourceEdgeWriter` (`1.81 ms/op`, `3.89 MB/op`) and
`BenchmarkObservabilityCoverageEdgeWriter` (`1.71 ms/op`) baselines on the same
input shape and machine, because the row carries fewer properties and a single
static relationship type. `go test ./internal/reducer
./internal/storage/cypher ./internal/storage/postgres ./cmd/reducer -count=1`
proves the exact-only, idempotent-reprojection, readiness-gating,
digest-unresolvable-no-dangle, empty, stale, and owner-reference-excluded cases.
Observability Evidence: the new `eshu_dp_kubernetes_correlation_edges_total`
counter (dimension `resolution_mode`), the `kubernetes correlation
materialization completed` structured log with per-stage durations, edge count,
and `skipped_unresolvable_source`, the
`reducer.kubernetes_correlation_materialization` span, and the
InstrumentedExecutor's `eshu_dp_neo4j_query_duration_seconds` /
`eshu_dp_neo4j_batch_size` on each `phase=kubernetes_correlation_edge` /
`label=RUNS_IMAGE` statement let an operator see live-workload edge throughput
and spot a generation that materialized zero edges, at 3 AM.

## EshuSearchDocument curated search read model (issue #2236)

`DomainEshuSearchDocument` (`eshu_search_document_domain.go`) is the design-430
curated search projection, kept separate from canonical graph writes:

- `ProjectSearchDocuments` (`eshu_search_document_projection.go`) curates a
  bounded per-generation source set (content entities, files, runtime summaries)
  through `searchdocs`, dropping sensitive or excluded candidates and returning
  documents ordered by id with a low-cardinality curation summary.
- `EshuSearchDocumentHandler` (`eshu_search_document.go`) drives one intent by
  streaming: it opens a write session, then `StreamSearchDocumentSources` feeds
  bounded keyset pages that are each projected, curated, and inserted
  incrementally; a single `Finalize` runs the authoritative retire. It aggregates
  the curation summary across pages and emits the canonical-write
  counter/duration plus a structured cycle log
  (`considered`/`included`/`skipped`/`written`/`retired`) with write subphase
  timings for fact upsert, index document upsert, term refresh, term upsert,
  fact retire, stale document retire, and stats upsert. The term refresh timing
  is the generation-scoped clear that runs before refreshed page terms are
  inserted.
  Streaming bounds peak memory to one page regardless of repository size
  (issue #3440).
- `SearchDocumentSourceLoader.StreamSearchDocumentSources`
  (`postgres.EshuSearchDocumentSourceLoader`) keyset-paginates the scope's
  repository content (entities by `entity_id`, files by `relative_path`) so each
  read is bounded by a `LIMIT` rather than loading the whole repository at once.
- `PostgresEshuSearchDocumentWriter` (`eshu_search_document_writer.go`) exposes a
  streaming write: `BeginEshuSearchDocumentWrite` returns a session whose
  `InsertPage` upserts one page (derived facts `reducer_eshu_search_document`,
  `truth_scope.level = derived`, keyed deterministically by scope, generation,
  and document id, plus persisted BM25 documents/terms) without retiring, and
  whose `Finalize` runs the single generation-authoritative retire over the union
  written-id keep-set and upserts stats. `WriteEshuSearchDocuments` is retained
  as a single-shot façade implemented over those primitives. It records bounded
  mutation/error/duration metrics plus `reducer.eshu_search_index_write`.
  `postgres.EshuSearchDocumentStore` reads back only the active generation, so
  superseded generations retire automatically.

Search documents are derived retrieval evidence; this domain performs no graph
write. No-Regression Evidence: the persisted index path adds no SQL statement,
queue, graph write, worker knob, or high-cardinality metric label; it records
OpenTelemetry signals from existing `ExecContext` results and the same SQL
shape. Observability Evidence: `go test ./internal/reducer/eshusearch -run
'TestWriteEshuSearchDocumentsRecordsSearchIndexTelemetry|TestWriteEshuSearchDocumentsRecordsSearchIndexErrors'
-count=1` and `go test ./internal/telemetry -run
'TestSearchIndexInstrumentsRecordBoundedLabels|TestSpanNames' -count=1`.

Observability Evidence: #4529 split the search-document writer's operator
signals by bounded write operation after a remote reducer proof showed
`eshu_search_document` handler time dominated the tail but did not expose which
Postgres mutation was slow. `EshuSearchDocumentWriteResult.Timings` feeds the
cycle log fields `fact_upsert_seconds`, `index_document_upsert_seconds`,
`index_term_refresh_seconds`, `index_term_upsert_seconds`,
`fact_retire_seconds`, `index_term_retire_seconds`,
`index_document_retire_seconds`, and `index_stats_upsert_seconds`.
`index_term_retire_seconds` is retained in the structured log for compatibility;
the current generation-clear lifecycle normally records term removal under
`index_term_refresh_seconds` instead.
`eshu_dp_search_index_write_duration_seconds` also carries the bounded
`operation` label so dashboards can separate `document_upsert`, `term_refresh`,
`term_upsert`, `document_retire`, `stats_upsert`, `page_total`, and
`finalize_total` without scope, generation, document, path, or term labels.
No-Regression Evidence:
`go test ./internal/reducer/eshusearch -run TestWriteEshuSearchDocumentsReportsSubphaseTimings -count=1`
fails without the timing fields and passes once every active write subphase
reports a positive duration in a delayed fake-DB proof.

Performance Evidence: the same #4529 branch proof reran the remote full corpus
against NornicDB PR #230 image `eshu-nornicdb-pr230:6bfaad33` and stopped after
37 completed `eshu_search_document` cycles because the bottleneck was already
unambiguous. The cycles spent 2,119.637s total, with 1,995.050s in
`index_term_upsert_seconds` alone; the slowest cycle spent 141.569s of 147.073s
in the term write. During those writes Postgres reported active `WALInsert`,
`WALWrite`, `BufferContent`, relation `extend`, and `DataFileExtend` waits.
The term writer now relies on the page-level refresh delete plus reducer queue
same-scope conflict fencing and inserts refreshed term rows without PostgreSQL's
`ON CONFLICT DO UPDATE` path. No-Regression Evidence:
`go test ./internal/reducer/eshusearch -run TestWriteEshuSearchDocumentsTermInsertAvoidsConflictUpdate -count=1`
fails if the term insert reintroduces conflict-update churn after the refresh
delete.

Performance Evidence: #4621 current-main proof after #4614
(`current-main-post4614-cap30-20260703T182320Z`, commit
`177e2beb6e33090fa2ac3882a12218e03277554e`, NornicDB main image
`eshu-nornicdb-main:5646d7ee`, 895-repository corpus, 30-minute cap) completed
source-local projection for all 895 repositories but stopped with reducer
backlog still open. In that window `eshu_search_document` completed 137 cycles
with 419 still pending, the persisted search index held 995,121 documents and
27,780,113 term rows, and `index_term_upsert_seconds` summed to 1,940.446s
(14.164s average, 251.732s max). The #4621 local preparation benchmark isolates
one in-process part of that timed subphase: on darwin/arm64 Apple M5 Max,
`go test ./internal/reducer -run '^$' -bench '^BenchmarkBuildSearchIndexTermColumns$' -benchmem -count=5`
over a 2,000-document / 150-term synthetic page (300,000 term rows) measured the
old global row sort at 119.220-131.184ms/op and about 169MB/op, while the
bucketed primary-key-order builder measured 34.029-34.492ms/op and about
92MB/op. Classification: handler win for term-column preparation only; wall
clock still requires a bounded remote proof on the branch.
No-Regression Evidence: `go test ./internal/reducer -count=1` covers the
streaming writer, term lifecycle, and COPY/fallback paths. The focused
regressions `TestWriteSearchIndexTermsCopyPathPreservesPreparedOrder`,
`TestBuildSearchIndexTermColumnsMatchesGlobalSortOrdering`, and
`TestBuildSearchIndexTermColumnsSortsUnorderedDocuments` prove the COPY helper
no longer performs a hidden full-row sort, while the page builder still emits
the same `(term_key, document_id)` primary-key suffix order as the old global
sort even when callers provide documents out of order.
No-Observability-Change: the change adds no metric, span, log field, queue,
worker, SQL statement, graph write, runtime knob, or high-cardinality label.
Operators still diagnose this path through the existing
`index_term_upsert_seconds` cycle log field and the bounded
`eshu_dp_search_index_write_duration_seconds{operation="term_upsert"}` metric.

`SearchVectorBuildRunner` is a side runner that can build derived vector rows
after search documents are active. The command layer wires it when the
semantic-search selector chooses either the deterministic local override or one
governed `search_documents` provider profile. This package owns the runner loop
and depends on narrow pending-list and builder ports. A sweep reads pending
active scopes, builds vectors in bounded document batches, and continues
through independent scope failures while returning a joined error for operator
visibility. The runner writes no graph truth and has no external vector-store
surface.

The production batch path keeps the ordinary 500-document per-scope limit while
20 or more scopes are pending. As the tail narrows, it divides a 10,000-document
budget across the remaining scopes, capped at 10,000 for one scope. Explicit
non-default limits remain fixed. This avoids rescanning a large final scope for
hundreds of 500-row sweeps without increasing the earlier multi-scope batch
cardinality.

Before each production build, the runner advances the vector-scope build fence
and carries that fence plus the observed document-projection revision through
the builder. Postgres checks those tokens again when each metadata and value
batch is written, so a delayed worker cannot overwrite a newer build after
ownership changes.

SearchVectorBuildRunner Evidence: `go test ./internal/reducer/searchvector -run
TestSearchVectorBuildRunner -count=1` proves bounded pending-scope
consumption, per-scope build calls, failure continuation with joined errors,
and dependency validation. `go test ./internal/reducer -run
TestServiceStartsSearchVectorBuildRunner -count=1` proves side-runner startup
through `Service.Run`; that wiring proof stayed at root when the runner moved
to `searchvector` in #6061 because it exercises `Service.startSideRunners`,
which the leaf package cannot reach without importing the reducer root. Cycle
logs include scanned scope count, attempted scope count, document count,
built vector count, policy-disabled document count, failed document count,
duration, and `failure_class=search_vector_build_error` when a pending scan or
build fails.
No-Regression Evidence: `go test ./internal/searchembedruntime
./internal/searchvector ./internal/storage/postgres ./internal/reducer
./internal/reducer/searchvector ./cmd/reducer -count=1` covers per-document
provider policy admission and disabled metadata convergence without reducing
runner concurrency. `./internal/reducer/searchvector` is listed explicitly
because `./internal/reducer` does not descend into child packages.

### Package relocation (#6061)

`SearchVectorBuildRunner` and its ports/request/result/config/identity types
moved from the reducer root into `internal/reducer/searchvector`.
No-Regression Evidence: pure relocation. In seven of the eight moved files the
package clause is the only line that changed — no import moved and no
identifier was re-cased. The eighth,
`searchvector/search_vector_build_runner_test.go`, also had
`TestServiceStartsSearchVectorBuildRunner` split out of it and left at root as
`search_vector_build_runner_service_test.go`, because that test exercises
`Service.startSideRunners`, which the leaf package cannot reach. Every method
body, struct field, constant value, and interface method set is byte-identical
to the pre-move root files. The root keeps
compatibility type aliases (`search_vector_build_compat.go`) for the
`Service.SearchVectorBuildRunner` field and the
`TestServiceStartsSearchVectorBuildRunner` wiring proof;
`go/cmd/reducer` was updated to import `searchvector` directly (aliased as
`reducersearchvector` in `search_vector_build_wiring.go` to avoid colliding
with the unrelated top-level `internal/searchvector` package it also
imports). `go test ./internal/reducer/... ./cmd/reducer -count=1`,
`go build ./...`, and `go vet ./...` all pass on the moved tree; the full
`TestSearchVectorBuildRunner*` and `TestServiceStartsSearchVectorBuildRunner`
selections list the same test counts before and after the move (26 tests in
`searchvector`, 1 at root — see the Evidence paragraph above for the exact
selection commands). No-Observability-Change: the sweep's structured logs and
`eshu_dp_search_vector_build_phase_seconds` histogram emission are unchanged
code, only relocated; `docs/public/observability/telemetry-coverage.md`'s
rows for this family were repointed to the new file paths in the same
change.

### search_vector_ready completion signal (#4673)

`RunOnce` optionally publishes a `search_vector_ready` completion signal
through the `ReadyPublisher` port (`SearchVectorBuildReadyPublisher`) after a
bounded sweep completes successfully. The gate is a POST-build re-check, not
the pre-build pending-scope listing: `publishReadyIfCaughtUp` re-queries
`ListPendingSearchVectorScopes` (bounded `Limit: 1`, just enough to know
whether ANY scope remains) after the build and publishes only when that
re-check finds zero. Gating on the pre-build count alone would miss the
sweep that drains the LAST pending scopes (non-zero pre-build count, but
truly caught up after the build) — the exact case the signal exists for. The
re-check runs after BOTH the batch-builder fast path (`SearchVectorBatchBuilder`,
what `searchVectorBuilderAdapter` in `cmd/reducer` wires in production) and
the serial per-scope path, so production — which uses the batch path —
actually publishes the signal. It never publishes after a failed sweep or
when the re-check itself errors.

The signal is keyed by `SearchVectorBuildIdentity` (provider profile, source
class, embedding model, vector index version) — the same tuple
`ListPendingSearchVectorScopes` and the builder ports key their work by. A
ready publish for one identity tuple never satisfies freshness for a
different tuple, which matters during a provider/model/index-version
rollout or when two reducer/API configs share one Postgres. The command
layer wires `ReadyPublisher` to `postgres.EshuSearchVectorBuildReadyStore`
(via a small adapter that converts between the reducer and postgres package's
identical-shaped identity structs — the reducer package stays free of
storage dependencies), which upserts an identity-keyed watermark row in
`search_vector_build_materialization`
(`go/internal/storage/postgres/migrations/040_search_vector_build_materialization.sql`,
`PRIMARY KEY (provider_profile_id, source_class, embedding_model_id, vector_index_version)`),
mirroring the existing `supply_chain_impact_winners_materialization` pattern
so the signal survives across the reducer/query process boundary. A publish
failure is logged (`failure_class=search_vector_ready_publish_error`) rather
than returned, since the bounded sweep itself already succeeded.

`go/internal/query`'s `SemanticSearchHandler` reads the identity-scoped
watermark through `PostgresSearchVectorReadyStore.SearchVectorReadyWatermark`
(configured with the process's own `SearchVectorBuildIdentity`, derived from
the same `searchembedruntime.Config` the vector search backend uses) and
downgrades the `POST /api/v0/search/semantic` truth envelope with the closed
`pending_search_vector` `FreshnessCause`
(`go/internal/query/freshness_causality.go`) when the watermark has never been
published or is older than a 2-minute freshness window (matching the
`SearchVectorBuildRunner` ~30s default poll cadence with headroom); the
`applySearchVectorFreshness` mapping lives in
`go/internal/query/semanticsearch/semantic_search_freshness.go`. The downgrade is gated to
vector-backed modes (`semantic`/`hybrid`, via `searchVectorBackedMode` in
`go/internal/query/semanticsearch/semantic_search.go`) — an explicit `mode:"keyword"`
request is served entirely by the deterministic lexical index and is never
downgraded by a pending search-vector build.

The reader does not trust an old watermark row by itself. It also checks the
current document projection revision against the identity-scoped vector scope
state and reports the signal absent while any active document projection is
building or failed, or while a non-empty ready projection's vector state is
missing, building, failed, or ready for an older revision. This closes the
interval in which a previously caught-up watermark survives a later projection.

Observability Evidence: before this change an outstanding search-vector build
had no attributable freshness cause on the search read path and no completion
signal an operator could watch. `TestSearchVectorBuildRunnerPublishesReadyWhenNoPendingScopesRemain`,
`TestSearchVectorBuildRunnerDoesNotPublishReadyWithPendingScopes`,
`TestSearchVectorBuildRunnerPublishesReadyAfterDrainingLastPendingScopes`, and
`TestSearchVectorBuildRunnerDoesNotPublishReadyOnBuildFailure`
(`searchvector/search_vector_build_ready_publisher_test.go`) prove the post-build publish
gating on the serial path;
`TestSearchVectorBuildRunnerBatchPathPublishesReadyAfterDrainingLastPendingScopes`
and `TestSearchVectorBuildRunnerBatchPathDoesNotPublishReadyWithPendingScopes`
(`searchvector/search_vector_build_runner_batch_test.go`) prove the same gating on the
production batch fast path. `TestSearchVectorReadyWatermarkIsIdentityScopedLive`
(`go/internal/storage/postgres/eshu_search_vector_build_ready_test.go`, gated
on `ESHU_SEARCH_VECTOR_READY_LIVE=1` + `ESHU_POSTGRES_DSN`) proves the
identity-keyed watermark against a live Postgres: a publish for identity A
does not create a row satisfying identity B. `TestApplySearchVectorFreshness*`
(`go/internal/query/semanticsearch/semantic_search_freshness_test.go`) prove the
watermark→envelope mapping including the probe-error-is-unavailable case;
`TestSemanticSearchHandlerKeywordModeIgnoresPendingSearchVector` and
`TestSemanticSearchHandlerHybridModeAppliesPendingSearchVector`
(`go/internal/query/semanticsearch/semantic_search_vector_freshness_mode_test.go`) prove the
mode gate. No-Regression Evidence: the post-build re-check and publish are a
single bounded (`Limit: 1`) re-query plus one idempotent identity-keyed
upsert, gated strictly after the existing `logResult`/`recordPhaseMetrics`
calls with no change to build behavior, concurrency, or throughput; verified
by `go test ./internal/reducer ./internal/reducer/searchvector
./internal/storage/postgres ./internal/query ./internal/query/semanticsearch
./cmd/reducer ./internal/mcp -race -count=1`.
The `semanticsearch` path is listed explicitly because the two mode-gate tests
cited just above moved there in #6060, and `./internal/query` does not descend
into child packages; `./internal/reducer/searchvector` is listed explicitly
for the same reason after the `SearchVectorBuildRunner*` ready-publish tests
cited above moved there in #6061 -- without either, this No-Regression run
stops covering the tests this section names.
