# #8750: size the destructive-successor guard like for like

## Problem

The #7926 destructive-successor backstop refuses a successor generation that
would empty or sharply shrink a scope. It compared the successor's streamed
fact count with every `fact_records` row of the prior generation, and it
sized a git delta successor by the delta's own stream. Both sizes were wrong.

1. **Reducer rows inflate the prior.** Reducer domains write
   `reducer_`-prefixed facts into the source generation after it activates.
   No collector stream carries them, so a prior on a reducer-heavy repository
   looked about twice its real size. A forced full that re-parsed the same
   repository held well under half of the raw total and was refused.
2. **A delta carries only what changed.** A git delta streams its changed
   files plus tombstones for deleted paths. The projector overlays it on its
   base full generation, so the projected result keeps every other file. Sized
   by its own stream, a 41-file delta looked like a 96 percent shrink.

A refusal rolls the commit back, keeps the prior generation active, and the
next sync recollects and is refused again. Nothing converges.

Observed on one hosted deployment before the fix (no names, public-safe):

- 19 of 20 git commits refused in 3 hours.
- 35 of 124 pushed repositories behind, median lag 5.7 hours, maximum 74.7
  hours.
- A forced full of 11,360 facts refused against 28,921 prior rows, of which
  15,231 were collector rows (11,360 is 0.75 of the collector rows).
- A 41-file delta of 6,544 facts refused against 175,858 prior rows, of which
  81,049 were `reducer_` rows.

## Fix

The branch's commits, by subject (SHAs change on rebase, so they are not
cited here): "size the successor guard on collector rows only"
(like-for-like prior), "size a delta successor by its projected file union",
"pin successor guard sizing and document it" (tests and docs), "probe on the
commit transaction and count unlisted tombstones" (review F2 and F3), "RED
for review F1", and "size the guard prior against the active generation too"
(the F1 fix, see "Pending window" below).

- `successorGuardCountQuery` reads three counts in one index-only pass over
  `fact_records_scope_generation_idx`: collector rows
  (`ExcludeReducerDerivedKinds`), all rows, and file rows. `CountFacts` is
  unchanged for its other callers.
- `backfill.DecideSuccessor` compares a full successor with the prior's
  collector rows only. The refusal predicate and the half threshold are
  unchanged.
- A git delta successor is sized by its projected file-level union against its
  anchor: the newest projected full generation at `DeltaBaselineCommitSHA`.
  Note (#8780): the anchor semantics are unchanged, but the anchor commit is
  now the delta's union base, `COALESCE(DeltaUnionBaseCommitSHA,
  DeltaBaselineCommitSHA)` (`deltaUnionCommitsFor`), the commit the projector
  resolves; `TestGuardAnchorsChainedDeltaAtUnionBaseLive` pins it.
  The anchor comes from `resolveDeltaBaseAtCommit`, the resolver the
  projector's overlay already uses. Path matching uses
  `facts.DeltaScope.FilePath`, the overlay's own qualifier.
- Zero-query lower bound: `anchorFiles - |touched ∪ live| + live`. At or above
  half the anchor's files, the delta stores. Under half, one probe of the
  anchor's file paths (`successorGuardAnchorFilePathsQuery`) gives the exact
  union, and the delta is refused only when `union * 2 < anchorFiles`.
- A delta with no anchor stores. The projector refuses it as base-missing and
  arms the reindex watermark, which a store-side refusal would not.
- A generation whose stream disagrees with `IsDelta` is sized as a full
  (the stricter rule) and logs `successor_delta_scope_mismatch`.
- Renames net to zero (old path deleted, new path changed). A deleted path
  that was never indexed removes no anchor file.

## Proof

`go/internal/storage/postgres/ingestion_successor_guard_sizing_live_test.go`
runs twelve live PostgreSQL tests through the production `IngestionStore`
commit path and the real projector claim. All twelve pass at the F1 fix
commit.

| Test | Pins |
| --- | --- |
| `TestGuardStoresForcedFullOverReducerHeavyPriorLive` | A forced full holding 0.74 of the prior's collector rows stores although it is under half of the raw total. The production refusal. |
| `TestGuardRefusesSharplyShrunkenFullOverReducerHeavyPriorLive` | A full under half of the collector rows is still refused. |
| `TestGuardStoresSmallDeltaOverLargePriorLive` | A 41-file delta over a large anchor stores. The production refusal. |
| `TestGuardRefusesMassDeleteDeltaLive` | A delta deleting 150 of 200 indexed files is refused on the exact union. |
| `TestGuardStoresRenameDeltaLive` | Moving 120 of 200 files fails the lower bound, and the exact probe stores it. |
| `TestGuardStoresDeltaDeletingNeverIndexedPathsLive` | 150 deleted paths that were never indexed remove no anchor file. |
| `TestGuardStoresDeltaWithMissingBaseLive` | A delta with no anchor stores and is left to the projector. |
| `TestGuardSizesDeltaWithoutRepositoryFactAsFullLive` | A generation claiming `IsDelta` with no `delta_generation` fact is sized as a full and refused. |
| `TestGuardRefusesSharpShrinkFullWhileChainedDeltaPendingLive` | L1, review F1: a 40-row full is refused while a chained delta (11 rows) is pending over a 411-row active union. |
| `TestGuardStoresHalfSizeFullWhileChainedDeltaPendingLive` | L2: in the same window a 208-row full stores (416 >= 411). Against the sum of both sides (422) it would be refused, so this pins max, not sum. |
| `TestGuardSizesAgainstLargerPendingFullLive` | L3: active 200 rows, pending full 800. Fulls of 40 and 300 rows are refused (300 passes the active alone); a 500-row full stores. |
| `TestGuardRefusesStepwiseShrinkThroughPendingFullLive` | L4: active 400, a pending full of 200 stores, then a 120-row full is refused (it passes the pending alone). |

The tests were written before each fix and run red first, on a disposable
local PostgreSQL 18 schema (`ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN`):

- On `0c966372f4`, before the like-for-like commit, the forced-full test failed with
  `destructive_successor skips = 1, want 0: 40 of 54 collector rows is not a
  sharp shrink`; the sharp-shrink test passed.
- With only the like-for-like commit applied, three delta tests failed with
  `destructive_successor skips = 1, want 0`: the 41-file delta, the
  never-indexed deletes, and the missing anchor. The mass-delete, rename, and
  missing-repository-fact tests already held and stay as guard rails.
- At the F1 RED commit, before the F1 fix, L1 failed with `gen-d stored = true,
  want false` and L4 with `gen-c stored = true, want false`. L2 and L3 held:
  they guard against over-refusal and against dropping the pending side.

## Pending window (review F1)

The duplicate check's candidate is the scope's newest pending or active
generation (`unchangedGenerationCandidateQuery`). The guard used it as its
prior. A second delta in a chain has no anchor (its baseline is the active
delta's commit), so it stores and stays pending until the projector refuses
it. A full that arrived in that window was sized against the pending delta's
own few rows and a sharp shrink stored.

A pending generation still matters to the guard, through the claim sweep.
`supersedable_projector_generations` (`projector_queue_claim_sql.go`)
retires an older pending generation once newer same-scope projector work
exists; a pending full is spared only by a newer delta (#7473). So a small
full stored in the window supersedes a larger pending snapshot unprojected
and then retracts the active generation. Neither side alone bounds what the
successor retracts.

The fix (arbiter ruling, option (e)): the prior is the larger of the active
generation and the candidate, by collector rows.

- The active read is `successorGuardActiveGenerationQuery`, built on
  `activeGenerationPredicate`. It is not `backfill.ActiveScopeGenerationQuery`,
  whose COALESCE over the newest row returns a pending generation when none is
  active.
- It runs only when the candidate is not active, in `successorPriorForGuard`
  on the base connection before the commit transaction opens (and on the
  transaction when the window recheck below re-sizes). When it returns
  the candidate id (activated between the two reads), no second count runs.
- Both sides are sized by one helper, `sizeGuardGeneration`: the union for a
  delta with a base, own rows for a base-missing delta (#8362).
- A pending candidate counts only while its projector work is live
  (`WorkLive`, ruling a9-126). A wedged one cannot activate, so next to an
  active generation it is not sized; with no active generation it is the only
  prior.
- `PriorOrphan` and the rule label follow the side that supplied the max. When
  both sides are empty the prior is orphan if either side is, keeping the
  #8362 empty-successor refusal.
- A failed active read or count fails the commit closed.
  `backfill.DecideSuccessor` is unchanged and pure; the delta anchor is
  unchanged.

Mutation check (each applied to the fix, run, and restored):

| Mutation | Tests that fail |
| --- | --- |
| Drop the active side | L1, L4, H1 `TestGuardPriorTakesTheActiveGenerationWhenItIsLarger`, plus H3, H4, H5, H6 (wedged case), H7 |
| Drop the candidate side | L3, H6 (live-pending case) |
| Max becomes sum | L2 |
| Invert the `WorkLive` gate | H6 (both active cases), L3, H1 |

The hermetic H tests are in `successor_guard_prior_test.go`.

Residual R1 (recorded, not blocking): a pending full P0 is stored, then a
pending delta P1 (which spares P0 under #7473), then a small full S, all before
any claim. S is sized against the larger of the active generation and P1's
union and can miss P0. It needs three generations in one unclaimed window, and
the guard is a coarse backstop by contract (`RefuseDestructiveSuccessor`).

### The pre-tx window and the in-tx recheck

Review (linuxdynasty, on `successor_guard.go`): the prior is sized on the base
connection before the commit transaction opens, so a same-scope generation
that commits or activates between that sizing and the commit leaves the guard
comparing against a stale, possibly smaller prior. That is a fail-open.

The window runs from the pre-tx reads to `upsertIngestionScope`, not to the
commit. `upsertIngestionScopeQuery` is `INSERT ... ON CONFLICT (scope_id) DO
UPDATE`, and the DO UPDATE arm row-locks the scope row until the transaction
ends. Arbiter census of every non-test writer of `scope_generations` status
or `activated_at`, or of `ingestion_scopes.active_generation_id`:

- Writers that can add or enlarge the prior wait on that row lock: another
  ingestion commit (the same scope upsert), the projector Ack (its first
  statement updates the scope row), the projector Fail (`FOR NO KEY UPDATE` on
  the scope row first), and the retirement fence (behind the per-repository
  deferred-maintenance key the commit already holds shared).
- Writers that only retire pending or failed rows (the claim sweep, the
  heartbeat supersede, the marker fence, the delta-baseline refusal, the
  wedged-active cleanup) take the fence row or generation rows `SKIP LOCKED`
  and never the scope row. A retirement after the recheck can only remove a
  sized pending from the real prior: the over-refusal direction, not a
  fail-open. Payload-only writers change no status, activation, or pointer.

So from the scope upsert to the commit no same-scope generation can be stored
or activated and the active pointer cannot move. Two shapes were reachable
before the fix: a second committer on the scope (a reshard replica, the
claimed host's concurrent workers, bootstrap-index or a CLI sync beside the
ingester) commits a large G2 in the window (W1); or the projector Acks a
non-candidate hinted G2 that a newer pending delta had spared (W2).

The fix (arbiter ruling, option 1): `recheckSuccessorPrior` runs in
`commitScopeGeneration` after the scope gate admits and before
`upsertScopeGeneration`, after which the successor's own pending row would be
the newest candidate. It is skipped when the prior is unused (no duplicate
check, or `TrustedDeletion`). On the transaction it re-reads
`unchangedGenerationCandidateQuery` and compares (found, generation id,
status, `WorkLive`) with the pre-tx candidate; only when the in-tx candidate is
pending it re-reads `successorGuardActiveGenerationQuery` and compares
(has-active, id) with the pre-tx active read, which `successorPrior` now
records explicitly. Equal on every field: the pre-tx prior stands. Any
difference: `successorPriorForGuard` re-sizes on the transaction (the same
helper, a delta's anchor included) and that result is used alone, never the
larger of the two, so the decision is a function of the committed state at
the lock and a `WorkLive` flip to false drops a wedged pending next to an
active (F1 decision 2). A failed read or count returns the error: nothing is
stored, refused, or enqueued.

Running the re-size on the transaction exposed a second defect, caught by W2:
the delta-union resolver held its target cursor open while it ran the base
query, and its base cursor open during the retention probe. On the pool each
statement gets its own connection; on a transaction pgx refuses with
`conn busy`. `readDeltaUnionTarget` and `readDeltaUnionBase` now close each
cursor before the next statement.

Proof, `ingestion_successor_guard_window_live_test.go` (local PostgreSQL 18,
`ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN`). A test `Begin` runs the interleave
once, after the pre-tx sizing returned and before the commit holds any lock:

| Test | Fixture | Before the fix | After |
| --- | --- | --- | --- |
| W1 `TestGuardRecheckRefusesAfterACommitInTheWindowLive` | A active 100; G2 full 1000 committed in the window; S full 60 | S stored | refused, sized on G2, `changed=true` |
| W1b `TestGuardRecheckStoresAHalfSizeFullAfterTheResizeLive` | same; S full 600 | stored | stored (1200 >= 1000) |
| W2 `TestGuardRecheckResizesAfterAnAckInTheWindowLive` | A active 100; G2 pending full 1000; P pending delta over A; Claim + Ack G2 in the window; S full 60 | S stored against about 106 | refused, `prior_sizing=active` on G2, active A to G2 |
| W3 `TestGuardRecheckUnchangedScopeReadsOnceLive` | no interleave; S 60 over A 100 | stored, no stage | stored, `changed=false`, `recheck_reads=1` |
| W4 `TestGuardRecheckScopeRowBlocksCommitAndAckLive` | S parked in its fact loop on an unbuffered stream | | see below |

W4 is the live proof of the freeze. With S holding the scope row, a second
commit and an Ack of the pending P, each on its own connection, both wait on
S's backend (`pg_blocking_pids`, `wait_event_type=Lock`,
`wait_event=transactionid`). Shape chosen: the Ack's transaction-local
`lock_timeout` (2 s default) expires, so it returns `ErrWorkAckDeferred` with
P still pending (observed 2.15 s after it was seen blocked); G2's commit has
no timeout and finishes only after S commits; the re-Ack then activates P.
S's recheck saw P as its candidate and no G2. S is parked after the
generation upsert, so W4 proves the freeze for the rest of the transaction;
the static census covers the stretch between the scope upsert and the
recheck, which holds the same row lock.

Hermetic, `successor_guard_recheck_test.go` (`guardRoutingTx` routes the
in-tx reads by query identity with staged in-tx answers, logs every exec and
query in order, and fails a read issued while another tx cursor is open, as
pgx does):

| Test | Pins |
| --- | --- |
| HR1 `TestGuardRecheckUnchangedActiveCandidateReadsOnce` | 1 recheck read, 0 active reads, 0 counts; pre-tx prior kept |
| HR2 `TestGuardRecheckUnchangedPendingCandidateReadsTwice` | candidate and active on tx, 0 counts |
| HR3 `TestGuardRecheckResizesWhenTheCandidateChanged` | counts for G2 and the active on tx; prior G2; stage ids |
| HR4 `TestGuardRecheckResizesWhenTheActiveChanged` | delta successor's anchor re-resolved on tx; no base-connection reads after Begin |
| HR4b `TestGuardRecheckResizesAPendingDeltaOnTheTransaction` | a pending delta candidate and a superseded base sized on one connection |
| HR5 `TestGuardRecheckDropsAPendingWhoseWorkWentIdle` | `WorkLive` true to false: prior is the active alone |
| HR6 `TestGuardRecheckFailureFailsTheCommitClosed` | candidate, active, or count error: no commit, refusal, or enqueue |
| HR7 `TestGuardRecheckRunsBetweenTheScopeLockAndTheGenerationUpsert` | scope upsert, then recheck, then generation upsert |
| HR8 `TestGuardRecheckParityWithThePreTxPath` | trusted deletion and no duplicate check: 0 reads; no candidate either side: 1 read |

Mutation check (each applied, the `TestGuardRecheck*` and `TestGuard*Live`
tests run, and the file restored; the working diff was byte-identical
afterwards):

| Mutation | Tests that fail |
| --- | --- |
| Drop the recheck call | W1, W1b, W2, W3, W4, HR1-HR8, HR4b (every recheck test) |
| Move it after `upsertScopeGeneration` | W1, W2, W3, W4, HR7, and L3 (`TestGuardSizesAgainstLargerPendingFullLive`) |
| Drop the active comparison | W2, HR4, HR4b |
| Drop `WorkLive` from the tuple | HR5 |
| Use max(pre-tx, in-tx) | HR5 |
| Restore the nested delta-union cursor | W2, HR4b |

The other commit-path fakes (`fakeTransactionalDB` with `fakeTx`, and the
proof-domain harness) model a scope with no concurrent writer: the
transaction mirrors the pre-tx candidate and active answers by query
identity, so those tests keep their decisions and their positional responses
are never consumed by the recheck.

No-Regression Evidence: the recheck adds one indexed single-row read to every
guarded commit (two when the in-tx candidate is pending) while the commit holds
the scope row: 0.019 ms execution for the candidate read and 0.004 ms for the
active read on a 3,001-generation scope (0.327 ms with 3,000 newer failed rows
ahead), and 0.48 to 0.86 ms of `successor_prior_rechecked` wall time per
unchanged-scope commit on local PostgreSQL 18; see the table under
"Performance Evidence" below. A re-size, which reruns the counts on the
transaction, happens only when the scope changed in the window. No lock, worker
count, or batch size changes.

Observability Evidence: every guarded commit that uses a prior logs the
`successor_prior_rechecked` commit stage with `changed`, `recheck_reads`, the
previous and in-tx candidate and active generation ids, and
`resize_duration_seconds` when it re-sized. The re-sized decision itself still
lands in `successor_guard` and
`eshu_dp_ingestion_successor_guard_decisions_total`.

R1 stays recorded: the recheck does not widen what the guard reads, so an
older spared pending full, or an un-hinted generation activated after the
lock, still projects under S. Un-hinted generations are never a candidate;
that is the separate issue the F1 ruling flagged.

## Performance Evidence:

The guard adds bounded reads on the ingestion commit path, so the claim is
bounded cost, not a speedup.

- Count query: one index-only pass per generation sized, replacing the
  previous single-count pass (`countFactsQuery`) with the same access path
  and three aggregates. A delta's anchor reuses a prior side's count when it
  is the same generation (review N1), so a delta pays a separate anchor count
  only when its anchor is neither the candidate nor the active generation.
- Anchor lookup: every delta resolves its anchor once (`deltaUnionBaseQuery`,
  the projector's own base lookup, about 0.33 ms warm per the #7797 note).
- Anchor probe: runs only when the zero-query lower bound fails, a delta that
  may remove most of its anchor's files. It reads the anchor's file rows once.
  Ordinary deltas and fulls never run it.
- Active lookup (review F1): only when the candidate is pending, one
  single-row read plus one count of the active generation (the count above);
  the pending candidate is counted too while its projector work is live. An
  active candidate, the common case, adds no read.

Measured on a hosted QA read replica (PostgreSQL in recovery, read-only
session), warm cache, median of five `EXPLAIN (ANALYZE, BUFFERS)` runs after a
warm-up, the old and new count interleaved:

| Generation | Old `countFactsQuery` | New `successorGuardCountQuery` |
| --- | --- | --- |
| 1,036,727 rows, 12,402 files | 118.2 ms, Index Only Scan, 20,489 buffers | 63.3 ms, Parallel Index Only Scan (2 workers), 20,614 buffers |
| 175,858 rows, 4,446 files | 56.4 ms, Index Only Scan, 39,316 buffers | 61.2 ms, Index Only Scan, 39,316 buffers |

Both use `fact_records_scope_generation_idx`. Heap Fetches are unchanged by the
query (33,348 and 175,863: the smaller generation's pages are not all-visible).
The large-generation gain depends on free parallel workers; the small one costs
4.8 ms (8.6%) for the extra aggregates.

The anchor probe on the 12,402-file anchor: Index Scan on the same index,
67,085 shared hits, 486.6 ms median execution (485.1 to 487.0). Nearly all of
it is detoasting `payload` to read `relative_path`: the same rows without
`payload` take about 6 ms; the payloads total about 207 MB. Cold cache was not
measured. This cost is paid only by a delta that may remove more than half of
its anchor's files; the zero-query bound keeps it off every other commit.

The active lookup on local PostgreSQL 18.6, a schema seeded with 23,021
generations (one scope with 3,040, one active and one pending), warm cache,
`EXPLAIN (ANALYZE, BUFFERS)`: `Index Scan using
scope_generations_active_scope_idx`, `Index Cond: (scope_id = 'scope-0')`,
`Filter: (activated_at IS NOT NULL)`, one row, 3 shared hits, 0.007 to 0.021
ms execution; a scope with no active row reads 2 shared hits in 0.009 ms.

In-tx recheck (#8750 window), local PostgreSQL 18, warm cache, median of nine
`EXPLAIN (ANALYZE, BUFFERS)` runs (`TestGuardRecheckReadPlansLive`), proof
scope with 3,001 generations beside 200 other scopes of 15 (6,002 rows):

| Read | Plan | Execution |
| --- | --- | --- |
| Candidate read, candidate active | Index Scan `scope_generations_scope_latest_lookup_idx` plus `fact_work_items_pkey` probe, 6 shared hits | 0.019 ms (0.018 to 0.035) |
| Candidate read, candidate pending | same, 6 shared hits | 0.019 ms (0.017 to 0.022) |
| Active read (pending candidate only) | Index Scan `scope_generations_active_scope_idx`, 2 shared hits | 0.004 ms (0.004 to 0.006) |
| Candidate read, 3,000 newer failed rows ahead | same index, 3,000 rows removed by filter, 113 shared hits | 0.327 ms (0.325 to 0.423) |

The common case adds one statement (two for a pending candidate) to every
guarded commit while it holds the scope row. The `successor_prior_rechecked`
stage measured 0.48 to 0.86 ms of wall time over five unchanged-scope commits
(W3, loopback connection, including the round trip and the log call); that is
the scope-row hold increase. A re-size runs the counts above on the
transaction only when the scope changed: 1.5 to 8.1 ms for W1's 1,000-row G2
over five runs.

Concurrency: the pre-tx prior counts, the active lookup, and the anchor count
run on the base connection before the commit transaction opens. The in-tx
recheck adds plain SELECTs on `scope_generations` and `fact_work_items` (and,
on a change, the counts and delta-union reads on `fact_records` and
`scope_generations`): no row lock, no new wait edge, and the lock order is
unchanged (advisory shared, scope row, reads, generation row, work row).
`TestIngestionCommitAndMaintenanceLockOrderingNeverDeadlocks` and
`TestProjectorCompletionDoesNotDeadlockSameGenerationCommit` still describe
the full ordering. The anchor probe runs on the commit transaction (review
F2), after the fact stream lands and before any projector work is enqueued. It takes none of the commit transaction's locks and
touches no claim, lease, or queue path, but the commit transaction stays open
while it runs, so a probed commit holds its shared deferred-maintenance barrier
and its inserted rows that much longer (up to the measured 0.49 s). The verdict is a pure function
of the values read (`DecideSuccessor`, `DecideDeltaFileUnion`).

## Observability Evidence:

- Metric: `eshu_dp_ingestion_successor_guard_decisions_total{kind,rule,outcome}`,
  one increment per guarded commit. `kind` is `full` or `delta`; `rule` is the
  closed `telemetry.SuccessorGuardRule*` set (`trusted_deletion`, `no_prior`,
  `orphan_fallback`, `empty`, `collector_ratio`, `anchor_missing`,
  `file_union_bound`, `file_union_exact`, `other`); `outcome` is `stored` or
  `refused`. No scope, repository, or generation labels.
- Log stage `successor_guard` on every guarded commit: `successor_kind`,
  `rule`, `refused`, `prior_collector_fact_count`, `prior_reducer_fact_count`,
  `prior_sizing` (`candidate`, `active`, or `own_rows_base_missing`),
  `prior_sizing_generation_id`, `prior_active_collector_fact_count`,
  `prior_candidate_collector_fact_count` (-1 for a side not sized),
  `successor_fact_count`. For deltas also `anchor_file_count`,
  `touched_path_count` (the decision's touched-or-live set, so
  `anchor_file_count - touched_path_count + successor_live_file_count`
  equals `union_file_lower_bound`), `manifest_path_count` (the repository
  fact's changed and deleted paths alone), `successor_live_file_count`,
  `union_file_lower_bound`, and, when the exact probe ran,
  `union_file_count` and `probe_duration_seconds`.
- Log stage `refuse_destructive_successor` now carries `rule`,
  `prior_reducer_fact_count`, `prior_sizing`, and `prior_sizing_generation_id`.
  Log stage `successor_prior_orphan_fallback` keeps `candidate_generation_id`
  and names the base-missing delta it sized in `prior_sizing_generation_id`. Log stage
  `successor_delta_scope_mismatch` marks a stream that disagrees on being a
  delta.
- Log stage `successor_prior_rechecked` on every guarded commit that uses a
  prior (#8750 window): `changed`, `recheck_reads` (1, or 2 for a pending
  in-tx candidate), `previous_candidate_generation_id`,
  `candidate_generation_id`, `previous_active_generation_id`,
  `active_generation_id`, and `resize_duration_seconds` when `changed` is
  true. No new metric: a re-sized decision is counted by the decisions
  counter above under its usual rule.
- The existing `eshu_runtime_refresh_skipped_by_reason_total{reason="destructive_successor_refused"}`
  still counts each refusal.

At 3 AM an operator who sees repositories falling behind reads the counter by
`rule` and `outcome`: a refusal rate on `collector_ratio` or `file_union_exact`
is real shrinkage or a collector fault, and the `successor_guard` line for the
scope names the counts that decided it. Coverage rows are in
`docs/public/observability/telemetry-coverage.md` and
`docs/public/reference/telemetry/metrics-ingestion-collectors.md`.

Related: `docs/internal/evidence/8362-uncountable-prior-guard.md` (the orphan
prior fallback this change keeps), `docs/public/operate/graph-delta-union.md`
(the anchor rule the guard shares with the union read).
