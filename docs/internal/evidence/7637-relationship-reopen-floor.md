# Relationship reopen replay floor (#7637)

The deferred maintenance pass listed every succeeded `deployment_mapping` and
`code_import_repo_edge` work item in the store and reopened the ones the
skip-set let through — including dead history on superseded generations and
rows on failed generations. Both listings now compose the same
per-scope replay floor and failed-generation exclusion as the shipped
correlation listing (`listSucceededReducerWorkItemsByDomainQuery`), from one
shared set of `scopeReplayFloor` SQL fragments. There is exactly one floor
text; the correlation query's composed text is byte-identical to its shipped
literal (proven by a temporary byte-identity test against the pre-change
extraction, since removed).

## Reopened-row counts (900 scopes x 25 generations, Postgres 18)

Seeded shim (`/tmp/7637-shim.sh`, queries extracted mechanically from the Go
source, no hand copies): 898 active scopes, 1 never-activated scope, 1 scope
whose latest generation failed. One succeeded row per (scope, generation,
domain), the shape the shipped comment documents.

| Listing (per domain) | Rows listed (= reopened candidates) |
| --- | --- |
| Old, unbounded | 22,500 |
| New, floored | 899 (898 active + 1 never-activated; failed scope excluded) |
| Excluded per domain per pass | 21,601 (21,600 below the floor + 1 failed-generation row at the floor) |

Both relationship domains together stop reopening 43,202 rows per pass at
this shape. The never-activated scope keeps reopening: its latest generation
is the floor.

## Listing cost: old vs new

`EXPLAIN (ANALYZE, BUFFERS)`, same host, back-to-back arms (`/tmp/7637-shim.out`):

| Arm | Execution | Buffers (shared hit) | Plan shape |
| --- | --- | --- | --- |
| Old | 11.589 / 11.532 ms | 605 (+125 planning) | Seq Scan + quicksort (1998 kB) |
| New | 24.330 / 25.053 ms | 2,788 | Materialized floor CTE + hash joins + quicksort (74 kB) |

The floored listing costs about +13 ms and +2,183 buffers per domain per
pass. That cost is deterministic in buffers; the wall figures are
same-host back-to-back runs on a shared box and carry its noise. The
listing is not where the pass spends its time: each reopened row costs one
client round-trip `UPDATE` plus a full reducer re-execution (see the
shipped comment's 91 ms listings vs 5.927 s pass). Removing 21,601
reopens per domain dwarfs +13 ms of listing.

## Differential

- New live tests: `TestRelationshipReopenSkipsSupersededGenerations`
  (superseded/failed stay `succeeded`; active/never-activated go `pending`,
  both domains) and `TestRelationshipReopenMatchesCorrelationReplayFloor`
  (the relationship partition sets equal the shipped correlation listing's
  set on the same fixture — derivation, not hand copy). Both failed on
  clean main and pass with the fix.
- `#7584` whole-vs-targeted differential still holds: `TestTheoryExactPartitionEvidenceClosure`,
  `TestTargetedMaintenanceOutcomesMatchWholePass`, and the terminal proofs
  pass, so evidence, phase, memo, and reopen rows for active generations
  are unchanged. One absolute expectation moved with the behavior:
  `null_active_pointer_after_projector_fail` no longer reopens the failed
  generation's relationship rows in either arm.

## #7584 step-3 comparison, re-labelled

#7645 is merged, so the separation is recorded here instead of editing its
body. At the step-3 shape (900 scopes x 25 generations, k0), the whole-pass
arm's per-domain reopened rows split as:

| Line | Rows per relationship domain |
| --- | --- |
| Floor effect: below-floor + failed rows the old arm reopened | 21,601 (96.0%) |
| Live rows the new arm reopens (active + never-activated) | 899 (4.0%) |

The targeted pass's advantage over the whole pass in step 3 therefore
combines this floor effect with the targeting (exact-partition) effect;
only the 899 live rows per domain are within targeting's reach.

## Concurrency note

Unchanged. The reopen writes through `reopenSucceededReducerWorkQuery`,
a primary-key `UPDATE` with `AND status = 'succeeded'`. The reducer claim
path selects `status IN ('pending', 'retrying', 'claimed', 'running')`
(`claimReducerWorkQuery`, `reducer_queue_batch_query.go`) and never sees a
`succeeded` row, so the reopen's row locks cannot block a claim. This
change only narrows which `succeeded` rows are listed; the lock class is
identical.

## Adjacent repairs in this PR

- The shared reopen proof schema predated the queue capability columns, so
  every test driving the real `ReopenSucceeded` failed with SQLSTATE 42703.
  `provisionReopenPartitionMemoSchema` now layers
  `reducerClaimCapabilityColumnsSchemaSQL` plus migration-088 `reopened_at`.
- Three correlation floor tests seeded `supply_chain_impact`, which
  completion-driven replay removed from the reopen list; they failed on
  main. Re-seeded to `container_image_identity`, the remaining fact-backed
  reopened domain, preserving each test's rationale.

Performance Evidence: floored relationship listings return 899 rows against
22,500 unbounded per domain at 900 scopes x 25 generations (43,202 fewer
reopens per pass across both domains), at +13 ms / +2,183 buffers of
listing cost per domain; whole-vs-targeted differential green.

No-Observability-Change: no new metric, span, log key, or knob. The
existing per-domain `*_reopened` counters and `*_reopened count=`
log lines carry the after-floor counts an operator watches.

NOT_CHECKED: listing wall deltas on a quiet host (shared box; buffers are
the deterministic figure); reducer re-execution time saved per pass (the
shim measures listings, not the downstream drain).
