# Suppression Fan-Out Measurements (#10019)

Companion to [the design](10019-suppression-fanout.md). It holds the shim runs behind
its [Measurements](10019-suppression-fanout.md#measurements) section. The shims were
throwaway SQL on a private native PostgreSQL and are not committed.

Decision stated before each run. First run: can the diff run inline in the writer
transaction, or does it need an index, another shape, or an earlier cut? Threshold: p50 warm
under 500 ms, no whole-table sequential scan, buffers within about twice the rows read.
Second run: what does the walk cost at 1, 2, 4 and 8 adjacent pairs, which statement shape
is cheaper, and which cap `K` does the rule pick?

## The walk cost curve

Performance Evidence: run 2026-11-04 on a private native PG18.6, stock settings (`work_mem`
4 MB, `shared_buffers` 128 MB), full bootstrap schema at 0cb384ca89 (303 migrations). Two
operator-shaped scopes of 9 generations each, one generation per `INSERT` as the store
writes them: 100,000 suppressions per generation, and 1,000. Between adjacent generations 1%
of the suppressions change (every row with `i % 100 = k - 1` is edited at generation `k`,
one in twenty of those also moves `scope.cve_id`). Background rows: 300,000 EPSS and
200,000 finding rows across 20 holders. Generic plans (`plan_cache_mode =
force_generic_plan`), `EXPLAIN (ANALYZE, BUFFERS)` execution time, summed over the pairs of
a walk. Seven rounds; in each round every arm ran once, with the starting arm rotated, so a
round's arms share a load and rounds are not comparable with each other. The pairs are the
newest N of the nine generations.

| Round | Load1 at start | Canary ms |
|---:|---:|---:|
| 1 | 59 | 576 |
| 2 | 55 | 372 |
| 3 | 49 | 492 |
| 4 | 43 | 475 |
| 5 | 39 | 481 |
| 6 | 39 | 473 |
| 7 | 40 | 499 |

Control canary: `SELECT count(*) FROM generate_series(1, 3000000)`, run at the start of each
round (372-576 ms across the seven rounds, so the host speed varied by about 1.5 times).

| Suppressions | Pairs | Shape | Runs | p50 ms | Range ms | Mean load1 |
|---|---:|---|---:|---:|---|---:|
| 100,000 | 1 | one statement per pair | 7 | 550 | 493-739 | 46 |
| 100,000 | 1 | single GROUP BY statement | 7 | 969 | 828-1,173 | 46 |
| 100,000 | 2 | one statement per pair | 7 | 1,134 | 998-1,576 | 46 |
| 100,000 | 2 | single GROUP BY statement | 7 | 1,532 | 1,328-1,714 | 45 |
| 100,000 | 4 | one statement per pair | 7 | 2,044 | 1,933-3,128 | 45 |
| 100,000 | 4 | single GROUP BY statement | 7 | 2,372 | 2,301-2,706 | 46 |
| 100,000 | 8 | one statement per pair | 7 | 4,147 | 3,840-4,911 | 45 |
| 100,000 | 8 | single GROUP BY statement | 7 | 4,482 | 4,188-5,175 | 45 |
| 1,000 | 1 | one statement per pair | 7 | 3 | 3-4 | 45 |
| 1,000 | 1 | single GROUP BY statement | 7 | 11 | 7-11 | 45 |
| 1,000 | 2 | one statement per pair | 7 | 4 | 3-5 | 45 |
| 1,000 | 2 | single GROUP BY statement | 7 | 14 | 10-15 | 45 |
| 1,000 | 4 | one statement per pair | 7 | 8 | 7-9 | 45 |
| 1,000 | 4 | single GROUP BY statement | 7 | 23 | 17-23 | 45 |
| 1,000 | 8 | one statement per pair | 7 | 14 | 12-18 | 45 |
| 1,000 | 8 | single GROUP BY statement | 7 | 42 | 31-44 | 46 |

**Reading it.**

- One statement per pair is linear in the pair count, about 520 ms per pair at 100,000
  suppressions at load 45, and about 1.8 ms per pair at 1,000.
- The single `GROUP BY` statement is exact but never cheaper. It sorts every payload for
  `count(DISTINCT payload)`: 969 ms against 550 ms at one pair, and 1.1 times the pairs
  at eight. At 1,000 suppressions it is three times slower. The pairs shape wins.
- **No round here was quiet.** The lowest start load was 39, so the rule (the largest pair count
  whose median stays under 500 ms at load1 below 12) picks nothing, and the data cannot choose
  `K`. A watcher polled load1 every 30 s from 19:42 to 20:17 EST (70 polls) for a reading under
  12 held for 20 s, to rerun the rounds. The lowest reading was 18.3 and it ended at 86 to
  110, so no quiet window came. The quiet-host follow-up later ran as #10330 below; the
  options for `K` are in the design doc.

**The single statement.** Over a chain of N+1 generations, a suppression changed between some
adjacent pair exactly when it is missing from one generation or its payload varies:

```sql
SELECT stable_fact_key, array_agg(DISTINCT payload #>> '{scope,cve_id}') AS cve_ids
FROM fact_records
WHERE fact_kind = 'vulnerability.suppression' AND is_tombstone = FALSE
  AND scope_id = $1 AND generation_id = ANY($2)
GROUP BY stable_fact_key
HAVING count(*) < $3 OR count(DISTINCT payload) > 1;
```

On the 1,000-suppression chain of 9 generations the union of the eight pair diffs and this
statement returned the same 80 changed suppressions and the same 90 CVE ids (symmetric
difference 0).

**Plan.** One pair at 100,000 suppressions plans as a Hash Full Join over a Bitmap Heap Scan
on each side, each fed by a Bitmap Index Scan on `fact_records_scope_generation_idx`, no
sequential scan. The earlier fixture planned an Index Scan; both are index access on the
same index. The hash spills: 8 batches at 4 MB, temp 12,771 pages read and written (about
100 MB). Buffers: 16,835 pages for 200,000 rows read. Planning time was 44 ms on the one execution inspected.

**Fixture note.** This fixture inserts each generation in its own statement, as the store
does, so each generation sits contiguously on the heap (about 8,330 heap blocks for one generation).

## Quiet-host round (#10330)

Performance Evidence: run 2026-11-07 on a private native PG18.6 (Ubuntu 18.6,
stock settings: `shared_buffers` 128 MB, `work_mem` 4 MB), full bootstrap schema
at 52e0cbae93 (309 migrations). One operator-shaped scope of 9 generations with
100,000 suppressions each, seeded by one `INSERT ... SELECT` across all
generations, so generations interleave on the heap (pessimistic, like the
earlier fixture's g1-g5); 1% of the suppressions change between adjacent
generations (1,000 changed rows per pair, one in twenty of those also moves
`scope.cve_id`). Background rows in one separate scope: 300,000 EPSS and
200,000 finding rows. `VACUUM ANALYZE` after the seed.
`plan_cache_mode = force_generic_plan`; `EXPLAIN (ANALYZE, BUFFERS)` execution
time. The measured text is the shipped pair statement (the throwaway asserts the
wire source still carries its shape markers). Each statement ran under the wire's
own 10 s `SET LOCAL` bound, reset after, so the bound validation rides this
round. Seven rounds; in each round each arm ran once, with the starting arm
rotated. Row counts: 900,000 suppressions, 500,000 background.

| Round | Load1 at start | Canary ms | 1-pair ms | Walk-5 ms | Walk max single ms |
|---:|---:|---:|---:|---:|---:|
| 1 | 7.20 | 232 | 143 | 740 | 159 |
| 2 | 6.79 | 232 | 141 | 736 | 151 |
| 3 | 6.79 | 232 | 143 | 786 | 175 |
| 4 | 6.79 | 253 | 142 | 779 | 167 |
| 5 | 6.79 | 232 | 144 | 735 | 152 |
| 6 | 6.40 | 241 | 145 | 790 | 191 |
| 7 | 6.40 | 232 | 143 | 729 | 152 |

Control canary: `SELECT count(*) FROM generate_series(1, 3000000)`, run at the
start of each round (232-253 ms across the seven rounds, so the host speed was
stable). Every round started under the repo timing rule (load1 under 9), so this
round qualifies where the earlier ones did not.

**Reading it.**

- One pair at 100,000 suppressions: p50 143 ms, max 145 ms (the p95 estimate over
  seven rounds), range 141-145 ms. Under the 500 ms acceptance with margin.
- The capped walk at K=4 over the 9-generation chain (endpoint g1-g5 plus pairs
  g5-g6, g6-g7, g7-g8, g8-g9, the exact plan `planSuppressionWalk` issues):
  p50 740 ms, max 790 ms.
- (K+1) x pair-max = 5 x 145 ms stays far inside the 5 s headroom the 10 s bound
  needs (condition 2 acceptance met in the same round).
- The pair plan is unchanged from the loaded host: Hash Full Join over
  `fact_records_scope_generation_idx` on both sides (Bitmap Heap Scan plus Index
  Scan), no sequential scan. Temp spill was not re-measured here; the wiring
  figure (about 104 MB per pair at 4 MB `work_mem`, peak one pair) stands.
- A larger cap was not probed on a quiet host, so K stays at 4 with the headroom
  now proven instead of provisional.

## One pair, earlier runs

Performance Evidence: run 2026-11-04 on a private native PG18.6, stock settings, full
bootstrap schema at 73032877fd (302 migrations). Six generations of 100,000 suppressions, plus
300,000 EPSS and 200,000 finding rows across 20 holders. g6 against g5 changes 1,000
suppressions: 600 edited (50 moved CVE), 200 removed, 200 added. Seven interleaved runs per
arm with a rotating first mover. Times are the median of seven. Rows from separate rounds
are not comparable with each other, since the load differed.

| Round, 1-minute load | Arm | Median ms | Range ms |
|---|---|---:|---:|
| A, about 32 | custom plan | 483 | 425-801 |
| A, about 32 | generic plan | 518 | 476-806 |
| B, about 10 | generic, 4 MB `work_mem` | 321 | 317-350 |
| B, about 10 | generic, 64 MB `work_mem` | 193 | 185-196 |
| B, about 10 | generic, sha256 of payload text | 481 | 468-495 |
| C, about 17 | generic, 4 MB `work_mem` | 439 | 375-500 |
| D, about 49 | generic, 4 MB `work_mem` | 666 | 503-1257 |
| D, about 49 | generic, `jsonb_hash_extended` | 627 | 502-658 |

| Other read | Result |
|---|---|
| Diff plan | Hash Full Join over two index scans on `fact_records_scope_generation_idx`; no seq scan |
| Diff buffers | 50,172 pages for 200,000 rows read; 1,000 returned (all 50,169 pages read from the OS cache; `shared_buffers` was 128 MB) |
| Temp spill at 4 MB | 13,019 pages, about 104 MB; none at 64 MB |
| Candidate read, 6 generations | 0.16 ms median (range 0.13-0.21), tiny-table seq scans |
| No-baseline first pass, 100,000 rows | 33 ms median, 8,420 pages |
| Distinct CVE ids reaching the holder read | 1,028 with the diff, 54,234 without |
| One 200-id holder read, 20 holders | 95 ms, 34,200 pages |
| Unchanged set, g5 against g4 | 0 rows |
| Lock modes during the diff | `AccessShareLock` only (148 relation locks) |

Against the threshold: buffers and plan pass. Time passes at load 10, 17 and 32 with the
custom plan, and misses at load 32 with the generic plan (518 ms) and at load 49 (666 ms).
The least loaded round gave 321 ms with a tight range. No round met the repo timing rule
(load1 under 9 with a control canary), so the timing is recorded as met on the least loaded
round with no qualifying quiet-host proof yet.

**The earlier fixture is pessimistic on one point.** g1 to g5 were inserted together and
interleave on the heap, so g5 costs 41,751 pages. g6 was inserted alone and costs 8,418.
The walk fixture above inserts one generation at a time and reads about 8,330 heap blocks per generation, which
matches the g6 figure.

**Rejected shapes.** Comparing a digest gains nothing: `jsonb_hash_extended` wrote the same
temp pages and took about the same time, and adds a false-equal risk that drops a change
silently. sha256 is slower. Raising `work_mem` for this statement is the only lever that
moved the number (193 ms at 64 MB), and it is not needed unless the quiet-host re-measure
misses.

**The bigger cost is elsewhere.** Inserting g6's 99,800 rows took 6.9 s on that cluster,
because Upsert rewrites the full set per change. At 100,000 suppressions the diff is a small
part of an operator write. The real suppression count is NOT_CHECKED.

## Tests committed now

`suppression_diff_test.go` and `suppression_walk_test.go` test the pure functions in
`suppression_diff.go` and `suppression_walk.go`; the wiring calls them from
`commitSupplyChainImpactRows` (`suppression_fanout_wire.go`), proved by
`suppression_fanout_wire_test.go` (writer transaction against faked reads) and the
live proofs in `supply_chain_impact_fanout_suppression_cveid_live_test.go`,
`..._suppression_wire_live_test.go`, and `..._suppression_composition_live_test.go`.

| Function | Covers |
|---|---|
| `changedSuppressionCVEIDs` | Added, removed, edited, re-scoped, no CVE, blank, Unicode whitespace, lower case, dedupe, order |
| `countSuppressionsWithoutCVEScope` | Limit 1's counter input |
| `unionSuppressionFanOutCVEIDs` | Unchanged set stays empty, suppression-only, overlap |
| `selectSuppressionDiffBaseline` | Plain chain, coalesced, late stale pass, never-activated, failed, no admission, first generation, ingest-order ties, activated_at and ingest order disagreeing, admitted-but-owed skipped for an older cleared admission, all-owed falls back to oldest retained, owed without admission never a baseline, current generation's owed marker ignored |
| `planSuppressionWalk`, `walkSuppressionDiff` | Net-zero plants against a brute-force oracle, the cap and its truncation count, the oldest-retained fallback, read errors, a 12-generation chain, candidates in any read order, a retained generation older than the baseline, and a suppression that names a CVE in one step and none in another |
| `decideSuppressionDiff`, `failClosedError` | KEV, EPSS, advisory, no facts, first pass, superseded-but-activated, pending, never activated, duplicate, recovery, mixed kinds; the pending outcome returns the existing deferral |
| `runSuppressionDiff` (wired) | Suppression-only union and owed recording, duplicate names nothing and reopens nobody, signal scopes issue no statement, recovery replays the walk, pending fails closed with no admission, no-baseline names every suppression, walk spans an owed generation, read error fails as the retryable fan-out class, the statement bound brackets the walk, one-hop still fans out suppression ids, retry repeats the recovery set |

**Mutants.** Each was applied in a private detached worktree on the committed tests, which
must fail on it and pass on the real code:

| Mutant | Killed by |
|---|---|
| Chain sort removed from `planSuppressionWalk` | `TestPlanSuppressionWalkSteps`, candidates in any read order |
| Range lower bound removed | `TestPlanSuppressionWalkSteps`, retained generation older than the baseline |
| Scoped exclusion removed from `countDistinctSuppressionsWithoutCVEScope` | `TestWalkSuppressionDiffCountsAndErrors`, named-then-unnamed suppression |

## Not checked

The full drain's wall time with the real fan-out store (the holder read was timed alone);
production `work_mem`, and a truly cold cache (the "cold" read was not OS-cold); real
generation layout on the heap, which the fixture only approximates.

Measured at wiring (not a quiet host, so not the K round): 260 OS holders deriving
through the full pipeline take 36 s for the first drain and 6 s for the 4-worker
concurrent drain of the capped continuation plus a newer generation
(`TestSuppressionFanOutCappedContinuationSurvivesNewerGenerationLive` logs both).
The 36 s is holder derivation, not the diff; the diff's own p50/p95 at the cap
was the condition-1 follow-up, now closed by the #10330 quiet-host round above
(pair p50 143 ms, capped-walk p50 740 ms).

## Wiring regression record

No-Regression Evidence: baseline is the pre-wire operator pass (no diff, empty
`ChangedCVEIDs`, holders reopen on the holders' own cadence). After measurement
on live proof Postgres 18.6 (disposable proof database,
`ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DISPOSABLE=1`): 260 OS holders, 256 reopened +
4 deferred at the 256-holder cap, 36 s first-pass derivation, 6 s concurrent
4-worker drain of the capped continuation plus a newer generation. Input shape:
operator scope, one `accepted_risk` suppression scoped `cve_id=CVE-X` per holder
CVE plus the unchanged-set, duplicate-delivery, retry-after-read-failure, and
newer-generation-before-continuation cases. The wiring adds at most K+1 bounded
diff reads (10 s `SET LOCAL` each, reset after) plus the admission and
candidate reads to the write path; scopes without suppression facts issue no
new statement. Safe because any diff read error or timeout fails the pass as
the existing retryable fan-out class with no partial set and no admission move.
Observability Evidence: `eshu_dp_supply_chain_impact_suppression_diff_total`
(outcome, baseline), `eshu_dp_supply_chain_impact_suppression_diff_duration_seconds`,
`eshu_dp_supply_chain_impact_suppression_walk_truncated_total`,
`eshu_dp_supply_chain_impact_suppression_no_cve_scope_total`, the
`supply_chain_impact.suppression_diff` span, one structured log line per diff,
and `suppression_diff_*` sub-signals beside `fanout_*`; the existing
`eshu_dp_supply_chain_impact_fanout_total{cause="suppression"}` shows the
reopens. `SupplyChainImpactFanOutFailureClass` is unchanged.
