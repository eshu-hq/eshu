# Partition-Scoped Deferred Relationship Maintenance (#7584)

Proof-branch note for the D3 slice of #7584. The entry point
`IngestionStore.RunDeferredRelationshipMaintenanceForPartitions`
(`go/internal/storage/postgres/ingestion_targeted_maintenance.go`) runs deferred
relationship maintenance for owed `(scope_id, generation_id)` partitions only.
Its only production caller is the activation-obligation consumer
(`go/internal/reducer/maintenance`) through `postgres.ActivationMaintainer`,
which runs only when `ESHU_ACTIVATION_OBLIGATION_CONSUMER_ENABLED` is set.

## Outcomes and refusals

Each owed partition gets one typed outcome in `TargetedMaintenanceResult.Outcomes`:

- `published`: the partition carries its `backward_evidence_committed` phase
  after the pass.
- `not_active`: the scope moved to another generation. This is decided before
  the catalog guard, so a superseded partition is never reported as refused.
- `inapplicable`: the partition is active but maps to no repository in the
  shipped active-repository read (a scope without a repository fact, or the
  scope that loses a repo_id `DISTINCT ON` collision). No pass can publish its
  phase. When the catalog guard admits the pass, its evidence work still runs,
  because a cloud scope's relations attach evidence to other repositories. When
  every active owed partition is inapplicable and the guard would refuse, the
  pass returns nil and writes nothing; that evidence work waits for the next
  whole pass, and the pass reports it (`SuppressedRefusal`, the
  `deferred_backfill_targeted_suppressed` log line and
  `duration_seconds{outcome="suppressed"}`).
- `retry`: an applicable active partition whose phase this pass did not
  publish (its generation advanced, or a guard skipped it).

Pass-level refusals return a typed error and write nothing:
`ErrTargetedMaintenanceCatalogChanged` (an active memo row records another
catalog fingerprint) and `ErrTargetedMaintenanceNoMemoBaseline` (no active
partition holds a memo row, so a catalog change cannot be detected; a fresh
install before its first whole pass, or an all-ArgoCD install). Both are held
by the consumer until the next epoch whole pass writes the memos and the phase.
For `catalog_changed` that pass is triggered by the ingestion commit that
changed the catalog. For `no_memo_baseline` there may be no such commit: the
hold lasts until any committed drain runs the whole pass. The same hold occurs
after the first whole pass whenever every memo-bearing scope advanced past its
memo, including a single-repository install's quiet generation; see #7638
item 9. `ErrTargetedMaintenanceClosureTooDeep`
reports a closure that did not settle in 8 promotion rounds. Every
`TargetedMaintenanceError` has a stable `Reason()` used as the telemetry label.

## Correlation reopen is partition-scoped

The pass reopens the cross-scope correlation domains only in the affected
partitions. The fleet-wide replay of those domains stays on the epoch whole
pass, unchanged, and is not part of the activation obligation (#7584):
the obligation owes the backward-evidence phase, while the correlation domains
wait on producer activation. A correlation consumer whose dependency on the
owed scope is not relationship evidence (for example a workload correlation
waiting on an OCI scope) is not reopened by this pass. The differential pins the
whole pass's extra rows outside the affected partitions, so a future widening
is visible.

## Contract

For every partition the pass touches, the committed
`relationship_evidence_facts` rows, `graph_projection_phase_state` rows,
`deferred_backfill_partition_memo` rows, and reopened `fact_work_items` equal
what `RunDeferredRelationshipMaintenance` produces for that partition. Outside
those partitions the pass changes nothing.

The touched set is computed, not assumed:

- Relationship resolution reads only the evidence attached to its own
  generation, and evidence is attached to the source repository's active
  generation. A change to an owed repository R therefore affects R's partition
  and the active partitions of the sources of evidence whose source or target is
  R.
- Sources are found with the target-catalog inbound loader
  (`loadAnchorScopedRelationshipFacts`, proven a superset by the read-side parity proof), grouped
  by fact partition. This finds other git repositories, GCP cloud scopes without
  a repository fact, and ArgoCD control repositories whose ApplicationSet reads
  an owed config repository.
- A source partition with no committed `backward_evidence_committed` phase has
  never been processed. It is promoted to owed so its own facts are loaded before
  its phase is published.
- An activation that changed the repository catalog fingerprint is refused with
  `errTargetedMaintenanceCatalogChanged`. A catalog change can alter evidence
  between repositories the owed partitions never touch.

The writes reuse the whole pass's own code: the memo-gated partition loader,
the batch writer (now taking its under-lock generation read as a parameter),
and the unchanged fan-in `publishDeferredBackfillPartition`. Every new query is
derived from the shipped query: one predicate inserted at a stable marker, or
the shipped query wrapped whole.
`TestTargetedMaintenanceQueriesDeriveFromShippedQueries` pins each derived
string byte for byte, and `TestBoundedRepositoryGenerationReadsMatchShippedRead`
pins the derived rows against the shipped rows, including two scopes that derive
the same repository id.

## Evidence

`TestTargetedMaintenanceMatchesWholePass` and
`TestTargetedMaintenanceInterleavingsMatchWholePass` seed two fully
bootstrapped schemas identically and run the real corpus-wide pre-pass to reach
steady state. They then activate the owed generation without a pass and run
the whole pass in one schema and the partition-scoped pass in the other. In the
touched partitions the rows must match with zero differences in both
directions. Outside them the partition-scoped schema must be unchanged. Each
fixture also states the evidence edges, phase rows and reopened work items it
expects. The fixtures cover a direct outgoing reference, inbound content, a
GCP relation (owed cloud scope, and inbound to an owed repository), an ArgoCD
ApplicationSet with an external config repository, two repositories in one
partition across separate batches, empty evidence, an unrelated quiet scope,
memo hit and miss, a generation advance before the evidence commit and before
the phase, successor activation, a failed sibling batch and its retry, an
unprocessed inbound source, a catalog change, a superseded owed partition under
a catalog change, the no-memo baseline, a NULL active pointer from the real
projector Fail path, an owed repo_id collision loser, and the empty-catalog
path.

The epoch whole pass is unchanged by the batch-writer refactor: the same rich
fixture, run through `RunDeferredRelationshipMaintenance` built from the base
commit and from this branch, commits the same 113 evidence, phase, memo and
work-item rows (set difference 0/0); a build with a mutated whole-pass loader
differs on 41 rows.

The three review guards (reporting a suppressed refusal, counting refused
partitions separately from retries, and a non-recording span without a
tracer) are mutation-proven against the live outcomes
fixture and the span unit tests: not setting `SuppressedRefusal`, counting a
refused pass's held partitions as `retry`, adding the pass outcome to
`outcomes_total`, marking a typed refusal as a span error, and writing onto the
caller's span without a tracer each fail a named test.

## Performance and observability

No-Regression Evidence: the whole pass is unchanged at runtime. Its batch
writer now takes the under-lock generation read as a parameter, and the whole
pass passes `loadAllActiveRepositoryGenerations`, which calls the shipped
`loadActiveRepositoryGenerations` with the same query. The rest of the batch
transaction is the same code, and a base-versus-branch output differential of
the whole pass is 0/0 (see Evidence). The partition-scoped entry's only
production caller is the activation obligation consumer, which is off unless
`ESHU_ACTIVATION_OBLIGATION_CONSUMER_ENABLED` is set, so no default runtime
path runs it.

Performance Evidence: the cost harness measured the pass against the whole pass on a
restored template state (PostgreSQL 18, 4 CPU / 4 GiB, 25 generations per
scope, three samples per arm, arms interleaved with the first mover
alternating, 189 of 189 equality checks passing). Medians, variant k0 (one
quiet owed generation, no inbound source):

| Scopes | Whole pass wall | Partition-scoped wall | Ratio | Whole pass buffers | Partition-scoped buffers |
| --- | --- | --- | --- | --- | --- |
| 300 | 0.4034 s | 0.0917 s | 0.2272 | 62,563 | 6,652 |
| 600 | 0.8867 s | 0.1570 s | 0.1771 | 166,033 | 13,477 |
| 900 | 1.2365 s | 0.2114 s | 0.1710 | 378,716 | 20,283 |

At 900 scopes the ratio is 0.1727 with three inbound sources (k3) and 0.1892
with an ArgoCD ApplicationSet and an external config repository (argo). The
argo variant is marginal: its three pairs are 0.1843, 0.1849 and 0.2581 (whole
pass 1.178, 1.442 and 1.475 s; partition-scoped 0.304, 0.266 and 0.273 s).
The pass writes the same rows at every size: one evidence row (two for argo),
one phase and one memo row (four with three inbound sources), one reopened
item per correlation domain plus one `code_import_repo_edge` item, and it
wakes exactly the waiting row. The whole pass writes a memo and a phase row
per active scope and reopens about one item per scope per correlation domain.

The pass is NOT bounded by the owed partition's size. Its wall time and
buffers grow linearly with the number of scopes times retained generations:
0.20 ms of wall and 22.7 shared blocks per scope, against 1.39 ms and 527
blocks per scope for the whole pass, so 7.0 times flatter in wall time and 23
times flatter in buffers. Besides the catalog scan, the stale-memo check, the
phase-one anchor load and the wrapped partition read, two statements scale
with the corpus. (a) The derived correlation reopen listing
(`listSucceededReducerWorkItemsByDomainForPartitionsQuery`, opening with the
materialized `scope_replay_floor` CTE) is about 45% of the pass's buffers at
900 scopes. (b) A repository-bounded `latest_generations` `DISTINCT ON` read,
by its shape `activeRepositoryGenerationsForReposQuery`, is about 18%. Both
inherit a CTE over `scope_generations` and `ingestion_scopes` (and the
repository facts) that runs before the appended partition predicate. Moving
that predicate inside the CTE is a new theory that needs its own proof; it is
tracked with the consumer's default-on gate.

Separate lines at 900 scopes: a refused retry (catalog changed) takes 0.0519 s
and 3,338 blocks with exactly one catalog scan, and grows with the corpus
(1,064, 2,143 and 3,338 blocks at 300, 600 and 900 scopes). An inapplicable
obligation is retired in 0.0105 s and 46 blocks with no catalog scan. An idle
consumer poll takes 0.0106 s and 124 blocks. The whole pass is unchanged: its
output digest is identical at this branch and at the base at 900 scopes, and
its wall time was 1.2387 s against 1.2578 s (n = 1 per arm).

Limits of these numbers: the fixture has tiny facts (the whole pass takes
1.2 s at 900 scopes, far from a representative corpus); only three sizes ran;
the harness's own load1 guard was disabled because its template restores
pushed the one-minute load above its bound, while the host load stayed at
3.3 to 5.2 and the CPU canary guard stayed on. The consumer's lease is fixed
at 2 minutes (a maintenance deadline of 96 s) and is unmeasured on
representative facts; the slowest partition-scoped sample here, 0.30 s,
comes from the same tiny-facts fixture.

Observability Evidence: the pass records
`eshu_dp_deferred_backfill_targeted_duration_seconds{outcome}`,
`eshu_dp_deferred_backfill_targeted_outcomes_total{outcome}` (one per owed
partition, never per pass; a refused pass does not count its held owed
partitions as `retry`) and
`eshu_dp_deferred_backfill_targeted_reopened_total{domain}`. The pass outcome
(`completed`, `suppressed`, a refusal reason, or `error`) is the duration
histogram's `outcome` label, so its count is the pass count. It opens a
`relationship.backfill_deferred_targeted` span only when it is given a tracer
(without one it never writes onto the caller's span). Typed refusals are
designed holds and leave the span status unset; only an untyped failure records
the error and sets an error status. It logs one
`deferred_backfill_targeted_completed` line per pass, and logs one
`deferred_backfill_targeted_refused` line per applicable owed partition on a
refusal. The shared loader, memo gate, batch and fan-in code run with
instruments off, so the whole pass's `eshu_dp_deferred_backfill_*` and reopen
series count only the whole pass; the fixture `telemetry_counts_targeted_only`
asserts both halves.

## Known pre-existing behavior the pass reproduces

- A memo-hit partition that receives new evidence from a cloud scope keeps its
  relationship items succeeded, in both passes: the same-pass skip set keys on
  the partition's own fact load (fixture `cloud_scope_gcp_relation_owed`). The
  fix belongs in the shared skip-set construction.
- The deployment_mapping and code_import_repo_edge listings have no replay
  floor, so the whole pass reopens superseded generations' items on every pass.
- With a NULL active pointer (the active generation failed), both passes
  resolve the scope through `COALESCE(pointer, latest)` and publish a phase for
  the failed generation (fixture `null_active_pointer_after_projector_fail`).
  The consumer's finalize reads the raw pointer and retires the obligation.

## Not covered

- Cost on a representative corpus. The cost harness ran on a tiny-facts fixture
  (see Performance and observability).
- Concurrency at fleet scale. The composed proofs (an ingester pass,
  consumer replicas, lease expiry, scope-lock races, a crash before the phase)
  run on small fixtures in the activation live tests.
- Graph and API truth after the reducer replays the reopened items.
- The first-wins dedupe case in `DiscoverEvidence`: two envelopes that yield
  the same (kind, source, target, path, matched value) key with different
  details, one loaded and one not, could make the two passes write different
  evidence ids. No fixture covers it.

## Shared log events and refused-retry cost

The shared memo gate, batch writer, fan-in and scoped fact loader still emit
`deferred_backfill_partition_memo_gate_completed`,
`deferred_backfill_batch_committed`, `deferred_backfill_fanin_completed` and
the fact-load completion line with no path label, for both passes.
Metrics are already split; for logs, attribute a shared line by the pass line
that follows it: `deferred_backfill_targeted_completed` for the targeted pass,
`deferred_backfill_completed` for the whole pass. The memo-gate failure line
alone carries `path=targeted`.

A refused retry is not two corpus-sized reads but three: the
classification read (the wrapped `DISTINCT ON` over every repository fact,
moved ahead of the guard), then the catalog scan, then the stale
memo `EXISTS`. The cost harness measured that line with all three (Performance and
observability).
