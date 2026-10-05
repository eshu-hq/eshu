# Partition-Scoped Deferred Relationship Maintenance (#7584)

Proof-branch note for the D3 slice of #7584. The entry point
`IngestionStore.RunDeferredRelationshipMaintenanceForPartitions`
(`go/internal/storage/postgres/ingestion_targeted_maintenance.go`) runs deferred
relationship maintenance for owed `(scope_id, generation_id)` partitions only.
Nothing calls it yet; the activation-obligation consumer
(`go/internal/reducer/maintenance`) will reach it through a port.

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
hold lasts until any committed drain runs the whole pass. `ErrTargetedMaintenanceClosureTooDeep`
reports a closure that did not settle in 8 promotion rounds. Every
`TargetedMaintenanceError` has a stable `Reason()` used as the telemetry label.

## Correlation reopen is partition-scoped

The pass reopens the cross-scope correlation domains only in the affected
partitions. The fleet-wide replay of those domains stays on the epoch whole
pass, unchanged, and is not part of the activation obligation (#7584 ruling D1):
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
  (`loadAnchorScopedRelationshipFacts`, proven a superset in D3 step 1), grouped
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

## Performance and observability

No-Regression Evidence: the whole pass is unchanged at runtime. Its batch
writer now takes the under-lock generation read as a parameter, and the whole
pass passes `loadAllActiveRepositoryGenerations`, which calls the shipped
`loadActiveRepositoryGenerations` with the same query. The rest of the batch
transaction is the same code, and a base-versus-branch output differential of
the whole pass is 0/0 (see Evidence). The partition-scoped entry has no
production caller yet, so no runtime path runs it. Its own cost has not been
measured; that is D3 step 3, and no claim is made here.

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

- Cost. No timing or row-count comparison at 900 scopes has run (D3 step 3).
- Concurrency of this entry against an ingester pass, consumer replicas, or
  lease expiry (D3 step 4).
- Graph and API truth after the reducer replays the reopened items.
- The first-wins dedupe case in `DiscoverEvidence`: two envelopes that yield
  the same (kind, source, target, path, matched value) key with different
  details, one loaded and one not, could make the two passes write different
  evidence ids. No fixture covers it.

## Shared log events and refused-retry cost

The shared memo gate, batch writer, fan-in and scoped fact loader still emit
`deferred_backfill_partition_memo_gate_completed`,
`deferred_backfill_batch_committed`, `deferred_backfill_fanin_completed` and
the fact-load completion line with no path label, for both passes (review N4).
Metrics are already split; for logs, attribute a shared line by the pass line
that follows it: `deferred_backfill_targeted_completed` for the targeted pass,
`deferred_backfill_completed` for the whole pass. The memo-gate failure line
alone carries `path=targeted`.

A refused retry is not two corpus-sized reads but three (review N6): the
classification read (the wrapped `DISTINCT ON` over every repository fact,
moved ahead of the guard for review F3), then the catalog scan, then the stale
memo `EXISTS`. D3 step 3 must report the refused-retry line with all three.
