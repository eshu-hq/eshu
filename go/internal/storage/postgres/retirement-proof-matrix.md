# Retirement and Tombstone Proof Matrix

Issue #1800 (parent #1797). This is the single explicit retirement proof lane
that covers source-local truth (facts and per-generation reads) and
reducer-owned truth (graph edges and read models) together. It documents how
deleted, removed, tombstoned, and superseded evidence is kept out of
active-generation reads, names the test that proves each case, and records the
query shape that proves retirement does not require a broad graph scan.

## Two retirement mechanisms

Eshu retires evidence with two independent, layered mechanisms. Neither deletes
historical fact rows; both are read-time pointer/predicate filters, so audit
history survives while current reads stay truthful.

1. **Generation supersession.** Every source-local read joins
   `ingestion_scopes.active_generation_id = fact.generation_id` and
   `scope_generations.status = 'active'`. When a refreshed snapshot commits and
   the projector `Ack` five-step transition promotes the new generation, the
   prior generation is marked `superseded` and the scope pointer moves. Every
   fact in the superseded generation stops joining at once, with no per-row
   delete. This covers anything that simply is not present in the new snapshot:
   a removed file, a deleted or renamed entity, a dependency dropped from a
   manifest/lockfile, or a workload/deployment whose evidence was replaced.
2. **Tombstones.** Within the still-active generation, a fact whose
   `is_tombstone = TRUE` models collector-emitted negative evidence (a
   runtime/cloud/collector source object observed as gone while the rest of the
   generation is unchanged). Readers that add `fact.is_tombstone = FALSE`
   exclude it. This is a per-reader contract: supersession-only readers (for
   example `ListActiveRepositoryFacts`) intentionally do not carry the
   predicate, while identity/security readers (for example
   `ListActiveContainerImageIdentityFacts`) do.

Reducer-owned graph truth adds a third, complementary step: when a later
generation becomes active, the reducer **retracts** its prior edges (scoped to
its own evidence source) before rewriting the current generation's edges, so a
stale edge cannot accumulate.

## Matrix

| Candidate case | Mechanism | Fact families | Graph labels / edges | Read models | Query surfaces | Proof |
| --- | --- | --- | --- | --- | --- | --- |
| File removed from a repo | Supersession | `content`, `file`, `content_entity` | `:File`, `:Content`, entity nodes from removed file | active source-local reads, repository read model | HTTP `/repository` reads, MCP repo tools | `TestProofRetirementSupersededGenerationFactsAreNotReturnedByActiveReads` (storage/postgres) |
| Entity removed or renamed | Supersession | `repository`, `content_entity` | repository/entity nodes | `ListActiveRepositoryFacts`, repository read model | repo context surfaces | `TestProofRetirementSupersededGenerationFactsAreNotReturnedByActiveReads` (storage/postgres) |
| Dependency removed from manifest/lockfile | Supersession | `package_manifest_dependency`, package consumption facts | `:Package` consumption edges | active package-dependency reads (`facts_active_package_manifest_dependency.go`) | supply-chain / package surfaces | `TestProofRetirementSupersededGenerationFactsAreNotReturnedByActiveReads` (same supersession contract; package reads add `is_tombstone = FALSE`) |
| Workload/deployment evidence removed or replaced | Supersession | `repository`, workload-identity follow-ups | workload / deployment edges | workload + deployment read models | deployment trace, service story | `TestProofRetirementSupersededGenerationFactsAreNotReturnedByActiveReads`; supersession of the underlying generation proven by `TestProofDomainIncrementalRefreshSupersedesActiveGenerationOnChangedRerun` and `TestProjectorQueueAckPromotesGenerationAndSupersedesPriorActive` |
| Runtime/cloud/collector source fact tombstoned or stale | Tombstone | `oci_registry.image_tag_observation`, `aws_image_reference`, cloud runtime facts | container-image identity nodes/edges | `ListActiveContainerImageIdentityFacts` and other `is_tombstone = FALSE` readers | container-image, drift, security surfaces | `TestProofRetirementTombstoneInActiveGenerationIsFilteredWhenReaderOptsIn` (storage/postgres) |
| Reducer edge/read-model fact retracts after a later active generation | Reducer retract | `aws_iam_permission`, `aws_resource_policy_permission`, infra platform follow-ups | `CAN_PERFORM`, `PROVISIONS_PLATFORM` edges | reducer-owned graph + read models | IAM/security and platform surfaces | `TestProofRetirementReducerRetractsPriorGenerationEdgesBeforeRewrite`, `TestIAMCanPerformHandlerIdempotentReprojection`, `TestInfrastructurePlatformMaterializerRetractsStaleEdges` (reducer) |

## Empty- and first-generation safety

- **Empty generation** (no active pointer; first run not yet promoted, or all
  generations failed): active reads return nothing and never error. Proven by
  `TestProofRetirementEmptyGenerationIsSafe`. Generation-failure handling that
  clears the active pointer is proven by
  `TestProjectorQueueFailMarksGenerationFailedWithoutClearingOtherActiveGeneration`.
- **First generation** (only generation just became active): its facts are
  returned, and the reducer does **not** issue a spurious retract because there
  is no prior edge set. Proven by
  `TestProofRetirementFirstGenerationFactsAreReturnedAndNotPrematurelyRetired`
  (read side) and `TestIAMCanPerformHandlerSkipsFirstGenerationRetract` /
  `TestInfrastructurePlatformMaterializerRetractStaleEmptyRepoIDs` (reducer
  side). The first-generation skip is bypassed on a retried attempt so a partial
  prior write is still cleaned up: `TestProofRetirementReducerRetractsOnRetryEvenOnFirstGeneration`.

## Graph and API/MCP truth agreement

Active-generation source reads and reducer retract together keep the graph and
the query surfaces consistent: a superseded fact never reaches the projector's
active read, and a stale reducer edge is deleted before the new generation's
edges are written, so a graph read and the HTTP/MCP read model that derives from
it agree on the retired state. The query-surface SQL shape that enforces the
same `active_generation_id` + `status = 'active'` + (where applicable)
`is_tombstone = FALSE` predicates is asserted by
`TestProofRetirementProductionQueriesCarryRetirementPredicates` and by the
existing per-reader query-shape tests
(`TestFactStoreListActiveRepositoryFactsUsesActiveGenerations`,
`TestFactStoreListActiveContainerImageIdentityFactsUsesActiveIdentityGenerations`).

## No-Regression Evidence

No-Regression Evidence: this change is additive test + documentation coverage.
It adds no Cypher, graph write, queue, worker, lease, batching, runtime stage,
or schema DDL. The proofs exercise existing read and retract paths through the
in-memory queue/fact harness and the existing reducer handlers. Verified with
`cd go && go test ./internal/storage/postgres -run TestProofRetirement -count=1`
and `cd go && go test ./internal/reducer -run TestProofRetirementReducer
-count=1`, both green; each behavioral assertion was confirmed load-bearing by
mutating the emulated active-generation filter, the tombstone filter, and the
reducer retract guard and observing the matching test fail.

No-Observability-Change: no new route, span, metric, or label. Retirement
remains observable through the existing generation lifecycle signals — projector
ack/supersede transitions, `scope_generations.status`, the
`active_generation_id` pointer, generation-transition status rows, and the
existing reducer completion log (`skip_retract`, `retract_duration_seconds`).

## Performance Evidence (retraction does not require a broad graph scan)

Performance Evidence: retirement reads and retracts are index/pointer bounded,
not full scans.

- **Source-local active reads** join on `ingestion_scopes.active_generation_id`
  (one row per scope) and filter `scope_generations.status = 'active'`, then page
  by the stable `(observed_at, fact_id)` key with a bounded `LIMIT`
  (`listFactsByKindPageSize`). No predicate scans superseded generations; the
  active generation is selected by equality on the denormalized pointer column.
- **Reducer retract** deletes by relationship type plus an explicit
  `repo_ids` / scope-id list (for example the `PROVISIONS_PLATFORM` + `repo_ids`
  delete asserted in `TestInfrastructurePlatformMaterializerRetractsStaleEdges`,
  and the evidence-source-scoped `RetractIAMCanPerformEdges`). The delete is
  bounded to the reducer's own edges for the affected scopes, so it never walks
  the whole graph.

Because the work is additive proof coverage over existing index/pointer-bounded
shapes, there is no hot-path change and no new scan to measure; the evidence is
the query shape itself plus the bounded `LIMIT`/scoped `DELETE` the cited tests
assert.
