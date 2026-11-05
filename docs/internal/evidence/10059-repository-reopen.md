# Repository-keyed reopen for the manifest marker's gap passes (#10059)

Follow-up of #9874 (goal #9209, lane 3). When the marker's package-keyed diff
may have missed a removal — the prior view is unreadable, the walk exceeds the
32-generation cap, or a pruned intermediate is skipped — the pass reopens every
scope holding an active finding for the generation's repository, using the same
payload scan and statement timeout as `findSupplyChainImpactHoldersQuery`.

## Decision, threshold, deadline (stated before the probe)

- Repository read: proceed if the scan stays under 500 ms median at 10x of the
  finding census. 1x is 10,000 active findings (twice the 5,000-row
  affected-package census of `9874-lockfile-retrigger.md`; every
  affected-package census in the evidence directory is under 5,000 rows, and
  the #9874 seed already carried 100,000 finding rows as noise). 5x is 50,000,
  10x is 100,000. The bound is 10x the package-holder read's own 10x median
  (65.3 ms) with headroom for the `DISTINCT` sort, and 20x under the 10 s
  holder-read statement timeout the read runs under.
- Deadline: four hours from the start of the probe, then report raw numbers.

## What changed

- `SupplyChainImpactFanOut` gains `FindRepositoryFindingHolders(ctx,
  ownScopeID, repositoryIDs)`: active finding rows (`reducer_supply_chain_impact_finding`,
  non-tombstone) of the scope's active generation whose
  `payload->>'repository_id'` matches, other than the asking scope. The fact
  kind is a literal so the partial-index predicate of
  `fact_records_supply_chain_impact_repository_lookup_idx` holds under a
  generic plan. `TestFindRepositoryFindingHoldersQueryShape` pins the shape.
- The marker calls it only when `manifestChange.needsRepositoryReopen()` is
  set (`priorBaseMissing`, `degraded`, or `chainPruned`); clean passes keep
  exactly the two reads they had
  (`TestManifestTriggerSkipsRepositoryReadWithoutAGap`). The repository ids
  come from the current generation's own rows (`manifestRepositoryIDs`: the
  same `repo_id` the dependency read anchors on and the finding's
  `repository_id` carries). The holders join the same capped continuation
  list, so the completion stamp still waits for the drain, and a gap pass
  with no visible package change still runs the reads instead of stamping
  early.
- New result sub-signal `manifest_trigger_repository_holders` (count,
  reported on gap passes). The `prior_base_missing`, `baseline_degraded`, and
  `chain_pruned` signals keep their meanings; the telemetry pages now say the
  pass reopens the repository's holders instead of waiting for the advisory
  scope to rerun.
- Regression proof: `TestManifestTriggerPriorBaseMissingReopensRepositoryHolders`
  (RED before the fix: `reopened = []`), `TestManifestTriggerDegradedAndPrunedChainsReopenRepositoryHolders`,
  read-level `TestFindRepositoryFindingHoldersLive`, and end-to-end
  `TestManifestTriggerReopensRepositoryHolderOnMissingBaseLive` (a pin removed
  in a delta whose base is pruned reopens the finding's holder; another
  repository's holder is untouched).

## Holder-read probe (EXPLAIN ANALYZE after the SQL was written)

Method: PostgreSQL 18.6 in the lane container, disposable database bootstrapped
from `ApplyBootstrapWithoutContentSearchIndexes` (scratch probe
`zz_10059_explain_scratch_test.go`, deleted after capture). Synthetic data:
200 advisory scopes each with an active and a superseded generation; finding
rows spread round-robin across the scopes (`repository:r_checkout` is every
10th row), plus one superseded copy of every row so the plan sees retained
history. `VACUUM ANALYZE fact_records` before the runs. Three volumes, seven
runs each, `EXPLAIN (ANALYZE, BUFFERS)` of the shipped constant with
`$1 = {repository:r_checkout}`, `$2 = git-repo-checkout`. Host load average 6
to 18 during the runs (a CI gate ran concurrently for the 1x leg).

| Volume | Plan the planner picks | Execution, 7 runs |
| --- | --- | --- |
| 1x (10k active + 10k superseded) | bitmap heap scan on `fact_records_supply_chain_impact_repository_lookup_idx`, hash join to `ingestion_scopes` (801 buffers) | 2.05-2.11 ms, median 2.07 |
| 5x (50k + 50k) | nested loop over the 200 scopes, per-scope index scan on `fact_records_scope_generation_idx` (50,411 buffers) | 26.3-27.5 ms, median 26.6 |
| 10x (100k + 100k) | same nested-loop shape (96,958 buffers) | 52.7-91.5 ms, median 54.7 (first run cold at 91.5: 3,454 blocks read, 2,553 written) |

Verdict: PROCEED. The 10x median (54.7 ms) is 9x under the 500 ms bound
stated before the probe and 180x under the 10 s holder-read statement timeout
the read runs under. No new index: the 1x shape seeks the existing
`fact_records_supply_chain_impact_repository_lookup_idx`, and the 5x/10x
shapes probe per-scope through `fact_records_scope_generation_idx`.

Caveat: the seed holds 200 scopes, so the planner's scope-count switch was not
pushed to the many-scope bitmap shape the #9874 read showed at 20,601 scopes
(median 65.3 ms there). If the fleet grows past that shape, re-measure; the
read stays under the same 10 s timeout either way.

## Retention-hold alternative (ruled out with evidence)

The alternative named in the issue is holding a delta's base until the next
full generation's marker completes (the #7802 retention hold). Ruled out:

- Retention pruning is stamp-unaware. Only the manifest files reference
  `PayloadKeyManifestTriggerCompleted` (`supply_chain_impact_manifest_stamp.go`,
  `supply_chain_impact_manifest_generation.go`, plus the two live proof
  files); nothing under the retention path (`generation_retention_events.go`,
  `delta_union_base.go`) reads a work-item stamp. Teaching retention to hold
  a base on a per-scope reducer stamp couples a storage-wide policy to a
  domain-local lease-fenced payload key, and migrations are out of this
  lane's zone (as recorded for the #9874 read).
- The hold covers only one of the three triggers. A walk over the depth cap
  and a skipped pruned intermediate hide removals with every base present,
  so the repository-keyed reopen is needed regardless; the hold would add a
  storage-policy dependency while removing none of the new code.
- The reopen is bounded and already covered: one statement under the existing
  10 s timeout, holders drained through the existing capped continuation.

## Concurrency and replay

No new concurrency surface. The repository holders union into the same holder
list the cap splits and the continuation drains; only the claim that leaves no
holder behind writes the completion stamp (ruling D1, unchanged). A reopened
holder reruns once as a one-hop pass and fans out nothing. The in-flight read
still runs first, so a holder claimed or running when the marker passes is
flagged for one rerun.

## Telemetry

- `manifest_trigger_repository_holders` on the reducer success line (gap
  passes only).
- `docs/public/reference/telemetry/supply-chain-impact-winners.md` and
  `docs/public/observability/telemetry-coverage-reducer-domains.md` updated:
  the gap signals now describe the reopen.
- `HOLDER_FANOUT.md` known-limit section rewritten as the fixed behavior.

## Evidence markers

Performance Evidence: `EXPLAIN (ANALYZE, BUFFERS)` of the shipped
`findRepositoryFindingHoldersQuery` on PostgreSQL 18.6 (lane container,
disposable database from `ApplyBootstrapWithoutContentSearchIndexes`,
`VACUUM ANALYZE` before the runs), synthetic finding census at 1x (10,000
active + 10,000 superseded), 5x, and 10x (100,000 + 100,000) across 200
advisory scopes with `repository:r_checkout` on every 10th row; medians 2.07
ms, 26.6 ms, and 54.7 ms over seven runs each, against the 500 ms bound
stated before the probe and the 10 s holder-read statement timeout the read
runs under. Terminal row counts: 20,000 / 100,000 / 200,000 live finding rows
read back before the runs. The change is safe because the read seeks the
existing `fact_records_supply_chain_impact_repository_lookup_idx` (1x) or
probes per-scope through `fact_records_scope_generation_idx` (5x/10x) with no
new index, and holders drain through the existing capped continuation with
the completion stamp waiting for the drain.

Observability Evidence: new result sub-signal
`manifest_trigger_repository_holders` on the reducer success line (gap passes
only), asserted by `TestManifestTriggerPriorBaseMissingReopensRepositoryHolders`
and `TestManifestTriggerReopensRepositoryHolderOnMissingBaseLive`; the
telemetry pages and `HOLDER_FANOUT.md` now describe the reopen instead of the
gap.

## NOT_CHECKED

- Many-scope (20k+) bitmap plan shape for this query (seed held 200 scopes;
  see caveat above).
- Production finding-row census (volumes are synthetic, per the stated
  threshold).
