# #6184: cross-run resolver ordering + cross-repo CALLS readiness

Owner direction: fix the gate, don't weaken it. Two engine defects from the
#4594 breakdown, fixed here. The `Module` name-only key is already fixed on
main (`MERGE (m:Module {name, lang})`); `Environment` variance stays untraced
until the live rebuild gate runs again.

## Defect 1 — resolver preview reads fact arrival order

`aggregateCandidate` (`go/internal/relationships/resolver.go`) kept the first
five evidence facts it happened to see as the candidate's `evidence_preview`.
The order comes from `listEvidenceFactsByGenerationSQL` (`ORDER BY
observed_at, evidence_id`), where `observed_at` is one wall-clock timestamp
shared per `UpsertEvidenceFacts` call — so which artifacts exist is decided by
insert-call interleaving. Same defect behind the pass-1 under-production
(13 of 19) and the unstable pre-wipe reference.

Fix: `sortEvidenceFactsForAggregation` orders facts by clamped confidence
descending, then evidence kind, path, matched value, raw confidence, and the
`Details` serialization (total order; `fmt` prints maps with sorted keys, so
it is deterministic). One sort stabilizes all four order-sensitive
accumulations: the preview cap, rationale dedup, first-non-empty repo, and
first-wins field ties. The input slice is never mutated.

Regression: `TestAggregateCandidateEvidencePreviewIsOrderIndependent`
(un-skipped; failed before with the forward/reversed diff, passes after).
`TestAggregateCandidateSourceRevisionTiebreakKeepsFirstInInputOrder` pinned
first-in-input-order tie-breaks — that pin encoded the defect (input order is
wall clock), so it is rewritten as
`TestAggregateCandidateSourceRevisionTiebreakIsContentOrdered`: both input
orders yield the same revision.

```bash
go test ./internal/relationships/ -count=1          # ok 1.302s
go test -race ./internal/relationships/ -count=1    # ok 8.946s
```

## Defect 2 — cross-repo CALLS has no callee-side readiness key

Verified on the current base, not inferred:

- `storage/cypher/canonical_code_call_edges.go:68-70`: MATCH-only write. A
  row whose target uid has no node writes nothing and raises nothing.
- `reducer/code_call_projection_runner.go`: `MarkIntentsCompleted`
  unconditional after the write.
- `reducer/code_call_projection_selection.go`: readiness key built from the
  intent's own `AcceptanceKey()` (caller's repo) only. No key is ever
  constructed for the callee's repository.

Result on the DR fixture corpus: `CALLS` 115 of 116, reproducible — the one
cross-repository edge (`orders-api` into `lib-common`), recovered only by a
second full drain.

Fix: `CodeCallProjectionRunner` reports `BlockedReadiness` (existing
poll/backoff path) while `HasUncommittedCanonicalCodeScopes` is true — any
code scope whose active generation holds git `repository` facts but lacks
its `code_entities_uid` / `canonical_nodes_committed` phase. Non-code scopes
never emit repository facts, so they never block; tombstoned repository
facts do not count. No recovery-handler change was needed: refinalize
already clears `graph_projection_phase_state` for covered generations
(`storage/postgres/rebuildreset/reset.go`) and the projector republishes
phases unconditionally on re-run (`projector/runtime_stages.go:
writeCanonicalProjection` publishes on both the empty and written paths),
so during a rebuild code calls drain last and land cross-repo edges in a
single pass. The gate helper lives in `code_call_projection_work.go`
(`projectionLaneBlocked`) to keep the runner under the file cap.

Regression:

```bash
go test ./internal/storage/postgres/ -run TestReducerGraphDrain -count=1  # ok
go test ./internal/reducer/ -run TestCodeCallProjectionRunnerWaitsFor -count=1  # ok
go test -race ./internal/reducer/ -run TestCodeCallProjection -count=1  # ok
go test ./internal/reducer/ ./internal/relationships/ -count=1  # ok
go test ./internal/storage/postgres/ -count=1  # ok
go test ./internal/query/ ./internal/reducer/crossrepo/ ./internal/cli/compparity/ -count=1  # ok
go build ./...  # clean; go vet clean on touched packages
```

`TestCodeCallProjectionRunnerWaitsForCanonicalCodeQuiescence` fails without
the runner check (lease claimed, `BlockedReadiness == 0`).

## No-Regression Evidence:

- Baseline: #4594 evidence — 341 s rebuild on the Compose fixture corpus
  (67 scopes, 3,866 facts), `CALLS` 115/116, `EvidenceArtifact` settling at
  pass 2.
- After (unit level, this change): no hot-path Cypher change, no batch-size
  change, no worker-count change. Steady-state cost of the fix is one
  index-served `EXISTS` per code-call poll cycle:
  `fact_records_scope_generation_idx(scope_id, generation_id, fact_kind)`
  covers the repository-fact probe and the phase probe hits the
  `graph_projection_phase_state` primary-key prefix, over a scope-count row
  set with short-circuit on first match. No new index: a per-cycle,
  scope-count EXISTS does not meet the index doctrine's hot-and-wide bar.
- After (live rebuild level): PENDING — the local Docker daemon is wedged
  (buildkit EOF mid-build; container APIs EOF), so
  `scripts/verify-graph-rebuild-from-facts.sh` could not run here. It moves
  to the remote instance next; the rebuild-seconds before/after and the
  identity-diff verdict land there. Expected direction: code calls shift
  after canonical commits fleet-wide while the second full drain the
  runbook documents goes away, so net rebuild time should fall, not rise.
- Backend/version for the pending run: NornicDB pinned commit
  `3722b483c02c` (compose default), Linux amd64 remote.
- Input shape: the gate's own fixture corpus (same corpus as the 341 s
  baseline), terminal state both queues zero, identity-diff assertion.
- Why safe: the gate only delays edge writes until their endpoints'
  prerequisites exist; it changes which cycle an edge lands in, never which
  edges land. A wedged code scope stalls code calls visibly (pending intents
  + `BlockedReadiness`) instead of losing edges silently — the same trade
  the existing active-work drain check already makes.

## Observability Evidence:

No new instruments. A stall surfaces through existing signals: shared-intent
queue depth/age gauges hold pending code-call intents, and blocked cycles
record `BlockedReadiness` on the existing `recordCodeCallTiming` path — the
same visibility the active-work check provides. `graph_projection_phase_state`
gaps are queryable per (scope, generation) for drilldown. No-observability-change
beyond reuse: no new metric was warranted because the stall is already
observable.
