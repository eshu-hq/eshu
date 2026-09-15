# #6184: cross-run resolver ordering + cross-repo CALLS readiness

Owner direction: fix the gate, don't weaken it. Two engine defects from the
#4594 breakdown, fixed here. The `Module` name-only key is already fixed on
main (`MERGE (m:Module {name, lang})`). The current v1.3.2 live rebuild showed
no `Environment` identity difference; its remaining differences are recorded
below without expanding this readiness-focused increment.

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
- `go/internal/reducer/code/call/projection/runner.go`: `MarkIntentsCompleted`
  unconditional after the write.
- `go/internal/reducer/code/call/projection/selection.go`: readiness key built from the
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
`go/internal/reducer/code/call/projection/quiescence.go`
(`projectionLaneBlocked`) to keep the runner under the file cap.

Regression:

```bash
go test ./internal/storage/postgres/ -run TestReducerGraphDrain -count=1  # ok
go test ./internal/reducer/code/call/projection -run TestCodeCallProjectionRunnerWaitsFor -count=1  # ok
go test -race ./internal/reducer/code/call/projection -run TestCodeCallProjection -count=1  # ok
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

## Defect 4 — recovery retirement has a post-drain claim race

The reducer-drain poll and the retirement write originally had no common
exclusion boundary. A worker could claim pending, retrying, or expired work
after the final zero poll and then commit that claim before the retirement
statement. The retirement guard would correctly abort, but a claim that began
before retirement and committed after it could still run against a generation
recovery had already retired.

Fix: recovery now performs the long drain without a table lock, then takes a
transaction-scoped `SHARE ROW EXCLUSIVE` lock on `fact_work_items` and
immediately rechecks live leases before it enqueues or retires anything. The
mode conflicts with the queue's `ROW EXCLUSIVE` mutations and with another
recovery fence. It therefore closes the claim window and serializes concurrent
refinalizes without holding a lock while waiting for an existing worker.
The retirement `NOT EXISTS` predicate remains as defense in depth.

The live PostgreSQL regression pauses recovery after generation retirement and
uses the real `ClaimBatch` implementation on another connection. Before the
fence, all four cases returned a claim before recovery committed. With the
fence, pending, retrying, expired-claimed, and expired-running work each wait
on an ungranted `RowExclusiveLock`, then resume after commit. A second live
test starts two refinalizes together and proves they serialize without a
deadlock.

```bash
ESHU_POSTGRES_DSN=postgresql://... go test ./internal/storage/postgres \
  -run 'TestRefinalizeRetirementBlocksReducerClaimBatchUntilCommit|TestConcurrentRefinalizesSerializeWithoutDeadlock' \
  -count=1 -v  # ok, 6.714s
```

## Defect 5 — shared RUNS_ON identity could carry mixed provenance

Workload materialization and cross-repo resolution both write the same
`(WorkloadInstance)-[:RUNS_ON]->(Platform)` identity. The workload writer
preserved a foreign `evidence_source` on match but still overwrote `confidence`
and `reason`, leaving a tuple that belonged to neither writer.

The pinned NornicDB v1.3.2 behavior was measured before implementation. Reading
the relationship's existing properties in the same statement that `MERGE`s it
was not reliable, and `ON CREATE` did not persist the new relationship stamp.
The viable shape is two auto-commit statements: first `MERGE` only the identity,
then a bare `MATCH` with an ownership predicate that writes the complete
workload tuple and clears `source_tool`. Cross-repo continues to write its
complete tuple unconditionally, so it wins in every ordering.

The live Bolt matrix exercises workload-only, cross-repo-only, workload then
cross-repo, cross-repo then workload, workload/cross-repo/workload, and
cross-repo/workload/cross-repo. All six read back one coherent tuple:

```bash
ESHU_CYPHER_BOLT_DSN=bolt://127.0.0.1:... go test ./internal/storage/cypher \
  -run TestBoltRunsOnWritersPreserveOneCoherentProvenanceTuple -count=1 -v  # ok
```

## No-Regression Evidence:

- Baseline: #4594 evidence — 341 s rebuild on the Compose fixture corpus
  (67 scopes, 3,866 facts), `CALLS` 115/116, `EvidenceArtifact` settling at
  pass 2.
- Current-main comparison: unmodified `origin/main` `7ed966c45` on the same
  host and NornicDB v1.3.2 started at 2,530 nodes / 3,301 edges and rebuilt to
  2,521 / 3,284. It changed `CORRELATES_DEPLOYABLE_UNIT` from 3 to 8, lost one
  of 116 `CALLS`, and lost all four `HANDLES_ROUTE` and all four `RUNS_IN`
  edges. The identity differential was 29 missing / 12 extra. The next main
  commit, `62ce6e9fb`, changes tag-history query wiring only and does not touch
  projection or recovery behavior.
- Final post-review live run: `scripts/verify-graph-rebuild-from-facts.sh` used
  6,369 facts across 67 active scopes. The pre-wipe identity snapshot held 2,529
  nodes and 3,303 edges. The clean rebuild took 87 seconds (1m27s), drained both
  durable queues to terminal-zero, and held 2,530 node identities and 3,308 edge
  identities. It lost no identity; the differential was one additional workload
  instance and its five workload-instance relationships. It kept `CALLS=116`,
  `CORRELATES_DEPLOYABLE_UNIT=8`, `HANDLES_ROUTE=4`, and `RUNS_IN=4`.
- The interrupted pass killed the ingester, projector, and resolution engine
  only after graph rows existed with 1,010 items still active. The fresh-key
  recovery request ran while workers remained stopped, returned successfully,
  and only then restarted them. Both queues again reached terminal-zero. The
  result held 2,532 node identities and 3,312 edge identities. It lost no
  identity; the differential was three additional nodes and nine additional
  relationships, all in workload-instance materialization. The four owned lane
  counts again remained 116 / 8 / 4 / 4. The overall gate remained red because
  those sets differ from the older pre-wipe graph, so this increment does not
  claim full #6184 closure.
- This run also reproduced a NornicDB v1.3.2 scalar anomaly: after a rebuild,
  `MATCH (n) RETURN count(n)` and computed numeric projections could return no
  data even while label and identity scans returned more than 2,500 nodes. The
  interrupt checkpoint therefore uses the bounded row-existence probe
  `MATCH (n) RETURN labels(n)[0] ... LIMIT 1`; the full identity snapshots
  remain the correctness comparison.
- Crash recovery exposed a transport-budget inversion: reducer leases last 60
  seconds and the recovery fence waits up to five minutes, but the API formerly
  closed responses after 60 seconds. The live interrupted run then returned
  curl 52 even though the handler could still be waiting safely. The API write
  timeout now derives from the five-minute drain bound plus a one-minute margin;
  the successful immediate-recovery result above is the runtime proof.
- Runtime cost: the readiness gates add index-served `EXISTS` probes over
  scope-count row sets. The shared `RUNS_ON` workload batch adds one statement
  for eight fixture rows. The claim fence runs only in the recovery critical
  section after the queue has drained; it does not alter steady-state claim SQL,
  batch sizes, or worker counts. The final 87-second clean run was six seconds
  slower than the prior 81-second run on the same corpus and profile and far
  below the 341-second historical baseline; this sample is no claim of a
  speedup or a statistically significant regression.
- Backend/version: `timothyswt/nornicdb-cpu-bge:v1.3.2@sha256:a47ae7eadc80229d3109ade7a57dfc1f1504b7586798859e2b2ac6fc38897440`,
  Linux amd64 local.
- Input shape: the gate's own fixture corpus (same corpus as the 341 s
  baseline), terminal state both queues zero, identity-diff assertion.
- Why safe: the gate only delays edge writes until their endpoints'
  prerequisites exist; it changes which cycle an edge lands in, never which
  edges land. A wedged code scope stalls code calls visibly (pending intents
  + `BlockedReadiness`) instead of losing edges silently — the same trade
  the existing active-work drain check already makes.

## Observability Evidence:

The existing `eshu_dp_cross_repo_edges_resolved_total` counter retains its
routed-edge-only meaning and `relationship_type` label shape. The new
`eshu_dp_cross_repo_edges_dropped_total{reason="foreign_owned"}` counter records
ownership-partition drops separately, so a partition change is distinguishable
from graph-write loss without breaking established dashboards. A
readiness stall remains visible through shared-intent queue depth/age and
`BlockedReadiness`; `graph_projection_phase_state` gaps identify the blocked
scope and generation. The API recovery request remains covered by its existing
HTTP span and status code.
