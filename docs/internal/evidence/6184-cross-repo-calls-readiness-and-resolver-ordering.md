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
facts do not count. The check is wired UNCONDITIONALLY via a narrow
`CanonicalCodeQuiescenceChecker` dependency (`cmd/reducer/main.go`), not
behind the `ReducerGraphDrain` flag: the first remote gate run proved the
flag version a no-op where it matters — the DR compose stack sets no
`ESHU_QUERY_PROFILE`, so the profile parses to `""`, the drain stays nil,
and the cross-repo edge was lost exactly as before (115/116). The loss
happens on every backend/profile (MATCH-only write plus caller-only key)
while the contention half of the drain is NornicDB-local-authoritative
only. Exactly one checker runs per cycle, never both. No recovery-handler
change was needed: refinalize already clears `graph_projection_phase_state`
for covered generations (`storage/postgres/rebuildreset/reset.go`) and the
projector republishes phases unconditionally on re-run
(`projector/runtime_stages.go: writeCanonicalProjection` publishes on both
the empty and written paths), so during a rebuild code calls drain last and
land cross-repo edges in a single pass. The gate helper lives in
`code_call_projection_work.go` (`projectionLaneBlocked`) to keep the runner
under the file cap.

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

## Defect 3 — backfill snapshot predates projector activation

Fail-closed resolution (retryable, non-counting deferral while backward
evidence is uncommitted) turned a latent publisher race into a hard stall:
the golden-corpus gate drained to residual=8 and never converged.

Verified on a live kept gate stack, not inferred:

- `graph_projection_phase_state` held `backward_evidence_committed` for the
  previous generations only (29 of 31 scopes matched their active
  generation); the two retrying generations (`deployable-config`,
  `supply-chain-demo-db`) had no phase row.
- Maintenance pass 1 logged `deferred_backfill_completed evidence_facts=3
  readiness_rows=31` and `deferred_backfill_fanin_completed partitions=31
  published=31 skipped=0` — the pass believed every partition current.
- Timestamps: collection committed generation `45914e22` at `04:09:13.110`,
  the fan-in published at `04:09:13.795`, projector Ack activated the
  generation at `04:09:15.534` — 1.7 s after the snapshot.

Cause: collectors commit generations as pending; projector Ack activates
them (`activateProjectorGenerationQuery`, `updateProjectorScopeGenerationQuery`
in `go/internal/storage/postgres/projector_queue.go`) concurrently with the
deferred backfill. A scope whose activation lands after the snapshot keeps
no phase for the rest of the run.

Fix: `runPipelined` re-runs `BackfillAllRelationshipEvidence` after the
source-local projector drains, under phase
`relationship_backfill_post_drain`, before the reopen sequence. The
partition memo gate keeps the repeat cheap: unchanged partitions skip their
fact loads, so the repeat only derives evidence for newly activated
generations. The ingester's `RunDeferredRelationshipMaintenance` keeps its
single backfill — with no quiescence point there, the next periodic pass is
the covering pass by design; the non-counting retry bridges the gap and
supersession terminalizes items whose generation is no longer active.

Regression: `TestPipelinedBootstrapRunsCoveringBackfillAfterProjectorDrain`
fails without the second call (backfill calls = 1, want 2, plus a
post-quiescence entry assertion);
`TestPipelinedBootstrapCoveringBackfillFailureIsFatal` pins the fatal path.

Proof: `scripts/verify-golden-corpus-gate.sh` green with the fix —
`B-7 golden corpus gate green (elapsed 217s, budget ceiling 1800s)`, every
drain `fact_work_items_residual: residual=0`, zero
`backward_evidence_not_committed` occurrences in the run log.

## No-Regression Evidence:

- Baseline: #4594 evidence — 341 s rebuild on the Compose fixture corpus
  (67 scopes, 3,866 facts), `CALLS` 115/116, `EvidenceArtifact` settling at
  pass 2.
- After (unit level, this change): no hot-path Cypher change, no batch-size
  change, no worker-count change. Steady-state cost of the fix is at most
  two index-served `EXISTS` probes per code-call poll cycle per partition
  (active-work plus quiescence):
  `fact_records_scope_generation_idx(scope_id, generation_id, fact_kind)`
  covers the repository-fact probe and the phase probe rides the
  `graph_projection_phase_state` primary-key's `scope_id` prefix with
  generation/keyspace/phase as filters, over a scope-count row set with
  short-circuit on first match. No new index: per-cycle, scope-count
  EXISTS probes do not meet the index doctrine's hot-and-wide bar.
- After (live rebuild level), first remote run (flag-gated quiescence only):
  rebuild 16 s, 67 scopes, pre-wipe 2530 nodes / 3302 rels — but `CALLS`
  115/116 with the same orders-api → lib-common edge lost. Diagnosis: the
  DR compose stack sets no `ESHU_QUERY_PROFILE`, the profile parses to
  `""`, the drain stays nil, and the check never engaged. The check is now
  wired unconditionally (`CanonicalQuiescence`); re-run pending. Residual
  diff in that run (6 missing / 6 extra edges around `EvidenceArtifact`
  `application.yaml`, `CORRELATES_DEPLOYABLE_UNIT`, workload-instance
  deployment edges; 1 missing / 2 extra nodes) is triaged after the rewired
  run — those families sit outside the code-call lane. (Local Docker daemon
  was wedged — buildkit EOF mid-build — so the proof moved to the remote
  instance.) Expected direction: code calls shift after canonical commits
  fleet-wide while the second full drain the runbook documents goes away,
  so net rebuild time should fall, not rise.
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
