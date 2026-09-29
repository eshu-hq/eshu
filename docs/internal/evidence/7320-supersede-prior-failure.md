# #7320: superseding a failed or dead-lettered row erased why it failed

## Problem

Five statements set a `fact_work_items` row to `superseded`. Each one overwrote
`failure_class`, `failure_message` and `failure_details` with its own marker and
`updated_at` with the supersede time, and `status` left `dead_letter` or
`failed`:

| Writer | Rows it supersedes |
| --- | --- |
| claim sweep, `claimProjectorWorkQuery` (`superseded_stale_projector_generations`) | pending, retrying, failed, dead_letter on a pending or failed generation with a newer sibling; pending, retrying and expired-lease claimed/running on a superseded generation |
| Ack, `supersedeProjectorObsoleteGenerationsQuery` | pending, retrying, failed, dead_letter |
| Heartbeat, `supersedeRunningProjectorWorkQuery` | claimed, running |
| Ack refusal, `markProjectorAckSupersededQuery` | claimed, running |
| reducer claim and batch claim, `supersedeInactiveReducerGenerationsCTE` | pending, retrying, failed, dead_letter |

A claimed or running row is not clean: the retry path writes the generic class
`projection_retryable` (`projector_queue.go`) and the claim does not clear it,
so the cause lives in the retry's message and details, and the supersede erased
those too. `attempt_count`, `last_attempt_at` and `created_at` were never
touched, which is why 3,436 ops-qa superseded projector rows still show
`attempt_count = 3`.

Nothing downstream could recover the cause. `list_dead_letter_work_items` reads
`status = 'dead_letter'`, the status latest-failure read takes only
retrying/failed/dead_letter, replay never selects superseded rows, and the
generation lifecycle drilldown reports the supersede class as the "latest
failure" of the generation.

## Root cause and change

Root-Cause Evidence: the supersede `SET` lists assign the marker and rebuild
`failure_details` from scratch, so the old row values are dropped. Live
regressions in `supersede_prior_failure_live_test.go` failed on the unmodified
statements against PostgreSQL 18.6, each for the intended reason (the marker,
lease clear, `attempt_count`, `last_attempt_at`, `created_at` and the statement's
own detail keys all matched; only `prior_failure` was absent):

```text
--- FAIL: TestSupersedeFoldsPriorFailureClaimSweep/dead_letter
    prior_failure = <nil> (<nil>), want an object; details
    "{\"scope_id\": \"pf-claim-dead_letter\", \"work_item_id\": \"old-pf-claim-dead_letter\",
     \"generation_id\": \"pf-claim-dead_letter-gen-old\", \"generation_status\": \"failed\"}"
```

30 failing (sub)tests over the five writers; the 12 controls (rows that never
failed) passed before and after. After the change: 42 passing, 0 failing.

Every writer now appends one shared fragment to its `failure_details`, the
constant for its own alias (`priorFailureStaleSQL` or `priorFailureWorkSQL`,
the same text with `stale.` or `work.`; constants, so the statements stay
constant SQL). It adds one key, `prior_failure`, read from the OLD row:

```json
{"prior_failure": {"status": "dead_letter", "failure_class": "graph_write_timeout",
                   "failure_message": "...", "failure_details": "...",
                   "updated_at": "2026-09-28T10:09:00.123456+00:00"}}
```

- `failure_class` and `failure_message` stay the supersede marker, the contract
  pinned by the #7130 tests, `metrics.md` and the #6738 diagnosis. Existing
  `failure_details` keys keep their names and values.
- The key exists only when the old row is `failed` or `dead_letter`, or any of
  its three failure fields is non-blank. A pending or retrying row that never
  failed writes exactly what it wrote before.
- The old `failure_details` is embedded as a JSON string, verbatim, and is never
  cast to jsonb: it is free text or JSON, and one non-JSON row would abort the
  claim statement for every worker. NULL stays JSON `null`, distinct from `""`.
- No truncation in the fold. Truncated evidence is wrong evidence; the cost is
  measured below instead. (#7407 later bounded the text at the Fail-time writer,
  where the stored value and its copy stay equal.)
- No migration, no new column, no claim result-shape change, no new counter.
- `superseded` is in no supersede source set, so a row that holds `prior_failure`
  is never folded again; the revive paths null the failure fields first.

Read recipe (PostgreSQL 16 or newer):

```sql
SELECT work_item_id,
       CASE WHEN failure_details IS JSON OBJECT
            THEN failure_details::jsonb -> 'prior_failure' END AS prior_failure
FROM fact_work_items WHERE status = 'superseded';
```

Rows superseded before this change lost the evidence and are not backfilled.

Classification: correctness win with a measured cost. The fold adds about 7 to 8
microseconds, 986 bytes of WAL and 0.14 heap pages per superseded row that
carries 800 bytes of details, paid once per row. It is not a no-regression
result: the reducer claim statements are 22 to 26% slower on a sweep of 3,500
such rows. The projector claim sweep is the larger cost on this path, 2.93 s for
3,500 rows (0.84 ms a row) with or without the fold (#7408).

## Proof

- Matrix (`supersede_prior_failure_live_test.go`): each writer through its
  production entry point (`ProjectorQueue.Claim`, `Ack`, `Heartbeat`, the Ack
  refusal, `ReducerQueue.Claim` and `ClaimBatch`) over dead_letter, failed,
  retrying with evidence, failed with every field NULL, and never-failed
  controls, asserting `prior_failure` by value plus the marker, lease clear and
  untouched `attempt_count`/`last_attempt_at`/`created_at`.
- Text fidelity: plain text, JSON text, non-JSON text, quotes and backslashes,
  newlines, multibyte, the literal six characters `\u0000`, `""`, NULL and a
  1 MB value round-trip byte-identically through one claim.
- Terminal invariant: with the row made as attractive as possible (lease and
  generation predicates matching), none of the five statements rewrites a
  superseded row that already holds `prior_failure`.
- Guard: `TestSupersedeStatementsFoldPriorFailure` enumerates every
  `fact_work_items` UPDATE that sets `status = 'superseded'` from the Go source
  of the module, with no list kept in the test, and requires the fragment. It
  fails if it finds fewer than the five known writers. It strips SQL and Go
  comments first and requires the constant inside the `failure_details`
  assignment, so the constant's name in a comment, on another column or in a
  separate assignment does not count. Seeded violation: a planted writer without
  the fold, the comment and misplaced-constant variants, and the real
  `projector_queue_sql.go` with the fragment stripped, are reported; the clean
  tree passes. Cutting the
  fragment from the Ack-refusal statement in place made the tree scan fail, and
  restoring it made it pass.
- No-candidate claim (`TestSupersedeNoCandidateClaimRunsSamePlanAndWritesNothing`):
  with nothing stale, the three claim statements run the same plan with and
  without the fold, each supersede UPDATE writes 0 rows and nothing is superseded.
  Its seeded RED (`TestSupersedeNoCandidateProofRejectsAStaleRow`) plants one
  stale scope and must fail on "writes 0 rows". Numbers under Wall time.
- Harness PD label (`TestCostPDLoadLimitIsHalfTheCPUCount`, `TestCostPDLabel`,
  hermetic): the limit is half the CPU count (8 on 16, 9 on 18); a load equal to
  the limit or unreadable is NON-PD. RED against the old fixed 9: a start of 8.5
  on 16 CPUs was labelled PD.

## Concurrency

Conflict domain: the claim path's `fact_work_items` rows and the per-scope claim
fence. The fold adds no relation, no `FOR` clause, no join and no ordering to
any statement; only the `SET` expression grows, so the row locks, the
`FOR NO KEY UPDATE ... SKIP LOCKED` lock steps and the fence protocol of
#7108/#7115/#7130 are unchanged.

`TestSupersedeFoldsPriorFailureSeesConcurrentCommit` is the EvalPlanQual proof
for the one blocking statement, Ack's obsolete supersede. Session A moves a
snapshot-matching retrying row to dead_letter with new evidence and holds;
session B runs the statement and is observed waiting on the lock in
`pg_stat_activity`; A commits; B's `prior_failure` carries A's committed
`dead_letter`, class, message and details, not the retrying values B's snapshot
saw. PostgreSQL documents that the updater "proceeds with its operation using
the updated version of the row" (Read Committed, section 13.2.1).

The existing #7108, #7115 and #7130 live suites pass at 191a1dbffd (base
8996bcc1e7): the `TestSupersede`, `TestPriorFailure`, `TestProjector` and
`TestReducerQueue` suites with both live DSNs set on a throwaway PostgreSQL
18.6: 356 passed, 0 failed, 13 skipped (other DSNs; the cost and memory
harnesses are opt-in). Later commits on the branch change documentation only.

## Performance

Performance Evidence: the plan, the bytes and pages the fold writes, and the
memory it holds are proven by counts that do not depend on host load. Wall time
was measured on a quiet host (rule PD met: load1 below half the CPU count at
start, end and maximum; a control canary in every pair; two sets of nine pairs;
block below). At the ops-qa worst case of 3,500 superseded rows with 800-byte
details the fold adds about 7 to 8 microseconds per superseded row in every claim
statement: +0.8% on the projector claim (2.925 to 2.948 s) and +22 to +26% on
the reducer claims (0.106 to 0.134 s). This is a measured cost of keeping the
evidence. It is paid once per superseded row and grows with the kept bytes, and
costs about three times more per byte once the row is TOASTed (8 to 10
nanoseconds a byte at 800 bytes, 29 at 64 KB). Measured on stock PostgreSQL
defaults (shared_buffers 128 MB, wal_buffers 4 MB). The shipped Compose profile
is larger and was not measured.

Setup: PostgreSQL 18.6, worst case of 3,500 dead-lettered rows with 800-byte
details, each on its own scope, all superseded by one claim (ops-qa has one dead
letter with 793-byte details and 122,734 work rows), plus a stress case of 500
rows with 64 KB details. The "before" statement is the shipped text with the
fold cut out, derived by the test from the constant, so it cannot drift.

### Plan

`EXPLAIN (VERBOSE, COSTS)` of the projector claim with and without the fold, 320
lines each: identical node types and order; the total cost estimate moves
181814.66 to 181855.66 (+0.02%) and the sweep's row width from 52 to 904 bytes,
the old failure columns the target list now carries. `EXPLAIN (ANALYZE,
BUFFERS)` shows no spill for any statement in either case at the default
`work_mem`.

### Bytes and pages written (deterministic)

`TestSupersedePriorFailureWriteCost` runs each of the five writers, with and
without the fold, over identical freshly seeded rows, three repetitions with
alternating order, and reads what the whole statement wrote from
`pg_stat_statements` and the WAL position. It fails if a run did not supersede
every row or if `prior_failure` is missing (or present without the fold). Means
per superseded row, "steady" regime (pages already dirtied since the last
checkpoint), 800-byte details:

| Writer | WAL bytes before to after | WAL records | Buffers dirtied |
| --- | --- | --- | --- |
| projector claim sweep (3,500 rows) | 1787.7 to 2774.0 (+986.3) | 14.14, unchanged | 0.121 to 0.266 (+0.145) |
| reducer claim and batch claim (3,500) | 1510.1 to 2496.3 (+986.1) | 9.12, unchanged | 0.110 to 0.253 (+0.143) |
| Ack obsolete supersede (3,500) | 1566.8 to 2553.1 (+986.4) | 13.12, unchanged | 0.071 to 0.223 (+0.153) |
| Heartbeat supersede (500, one call each) | 1750.0 to 2728.4 (+978.4) | 14.08, unchanged | 0.100 to 0.254 (+0.154) |
| Ack refusal (500, one call each) | 975.1 to 1961.4 (+986.2) | 7.05, unchanged | 0.048 to 0.212 (+0.164) |

The plan-node view of the projector claim agrees: `EXPLAIN (ANALYZE, BUFFERS,
WAL)` reports 3,831,587 bytes of WAL before and 7,283,579 after (+3,451,992
bytes, +90%) for the same 28,304 records, buffers dirtied 257 to 763 (+506
pages, about 4 MB), shared hits 89,197 to 90,209 (+1.1%), no spill. After a
`CHECKPOINT` the first touch of each page also writes a full-page image; the
count of images is the same with and without the fold (for the projector claim
885 and 885), and the byte delta is the same +974 to +986 per row.

Where the +986 bytes and +0.145 pages a row come from: the fold embeds the old
800-byte `failure_details` a second time, as a JSON string, next to the four
other prior fields, so the rewritten row is about twice as wide. It stays under
the TOAST threshold at 800 bytes (no TOAST rows are written), so the extra
bytes go into the heap tuple and the new tuple versions fill about twice as many
pages, consistent with 0.121 to 0.266 pages a row. Every writer pays the same per-row delta to within 1%, which is the
expected shape for the same expression over the same input.

At 64 KB details (500 rows, projector claim, steady): WAL 3,574 to 73,730 bytes
a row (+70,156), WAL records 47 to 113, TOAST rows written 0 to 33 a row,
buffers dirtied +8.33 a row. The same fold, the same expression; the details
text is simply 80 times larger. 64 KB is not an observed size: the one ops-qa
dead letter has 793-byte details. It is not excluded either, because the Fail
path set no limit on the details it stored when this was measured. #7407 has
since bounded them at the writer (4096 bytes; see `7407-failure-text-bound.md`).
The fold itself does not truncate, so the cost here is measured, not capped.

### Why the projector fold cost differed from the reducer's in the first run

Two earlier readings did not survive: a 17-fold gap between the projector and the
reducer claim on the loaded host, and a 3.4-fold gap in an on-CPU shim on the
same host. The quiet-host run settles it: the fold costs about 6.6 microseconds a
row in the projector claim and 7.9 in the reducer claim. Neither the 17-fold gap
nor the 3.4-fold gap from the shim holds. The statements differ only in base
cost.

### Memory and temp files

`TestSupersedePriorFailureClaimMemory`, `EXPLAIN (ANALYZE, BUFFERS, VERBOSE)` in
a rolled-back transaction, per-node `Memory Usage`, `Batches`, `Disk` and temp
blocks summed over the plan. The claim statements set no `work_mem` of their own
(no `SET LOCAL`), so the server default of 4 MB applies, with the default
`hash_mem_multiplier` of 2 for hash nodes; a second run at the PostgreSQL
minimum of 64 kB shows how close each variant sits to spilling.

| Statement | Case | work_mem | Widest node before | Widest node after | Disk / batches |
| --- | --- | --- | --- | --- | --- |
| projector claim | 3,500 x 800 B | 4 MB | 705 kB (Hash, 8192 buckets) | 3,680 kB (same Hash) | none / 1 batch |
| projector claim | 5,000 x 800 B | 4 MB | 1,050 kB | 1,050 kB | none |
| projector claim | 8,000 x 800 B | 4 MB | 1,613 kB | 1,613 kB | none |
| projector claim | 500 x 64 KB | 4 MB | 94 kB | 502 kB | none |
| reducer claim and batch | 3,500 x 800 B and 500 x 64 KB | 4 MB | 25 kB | 25 kB | none |
| projector claim | 3,500 x 800 B | 64 kB | 121 kB | 121 kB | 1,120 kB, 8 batches, both variants |

The one place the fold widens a node is the projector sweep's hash at 3,500
rows: 705 kB to 3,680 kB, about 5.2 times, still inside the 8 MB a hash node may
use at the default (2.2 times of headroom); at 5,000 and 8,000 rows the planner
chooses a shape where the fold adds no node memory, and no case spills at the
default. Under a 64 kB `work_mem` both variants spill the same 1,120 kB and 8
batches, and the temp block count at 3,500 rows goes from 4,773,360 to 4,790,693
(+0.4%). The reducer statements do not hold the failure columns in any node. Not
measured: backend RSS, and a `work_mem` between 64 kB and 4 MB.

### Wall time (quiet host)

The cost is paid once per superseded row, because supersede is terminal. At 800
bytes it is about 7 to 8 microseconds a row in every claim statement. That is
+0.8% of the projector claim, whose sweep already costs about 0.84 ms per
superseded row, and +22 to +26% of the reducer claims, whose sweep costs about 30
microseconds a row. pg_stat_statements executor time moves by the same amount
(26.4 to 34.4 microseconds a row for the reducer claim), so the cost is inside
the statement. The WAL record count is unchanged; each record is about 986 bytes
larger, and one 3,500-row sweep fills the default 4 MB of WAL buffers 451 times in
the reducer claim and 386 times in the projector claim, against none without the
fold. At 64 KB details it is about 1.9 ms a row (500 rows: +0.95 s), which is 70
KB more WAL and 33 to 34 TOAST rows per row.

64 KB is not an observed size: the one ops-qa dead letter has 793-byte details. It
is not excluded either, because the Fail path sets no limit on the details it
stored when this was measured. The fold does not truncate. #7407 set the bound at
the Fail-time writer, so that the stored evidence and its copy stay equal.

A claim with no supersede candidates is the common case, and the deterministic
proof for it is `TestSupersedeNoCandidateClaimRunsSamePlanAndWritesNothing`. It
seeds 3,500 scopes whose only work row is pending on the current generation, runs
`VACUUM ANALYZE`, and runs `EXPLAIN (ANALYZE, BUFFERS, VERBOSE)` of each of the
three claim statements with the fold cut out and as shipped, in a rolled-back
transaction, each on a freshly seeded table (repeated rolled-back runs on one seed
change the reducer batch claim's join order, whichever text runs). Asserted: the
node sequence (type, operation, relation, index and CTE name) is identical, 108,
32 and 55 nodes; each supersede UPDATE writes 0 rows; a real run of either text
supersedes no work row. Logged, not asserted, on PostgreSQL 18.6 in a throwaway
container, before and after: top-node shared hits 28,520 and 28,517 (projector
claim), 17,750 and 17,750 (reducer claim), 45,824 and 45,824 (reducer batch
claim); planning time 1.28 to 1.83 ms for both texts of each. Seeded RED: with one
stale scope among the 3,500 the supersede UPDATE writes 1 row and the same checks
fail on "writes 0 rows" for both texts of all three statements.

The quiet-host run is one command per case, 9 or more before/after pairs,
interleaved with alternating first mover, each statement with a same-text
control, the run labelled PD only if load1 stayed below half the CPU count at
start, end and maximum:

```text
cd go && ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN=<disposable postgres> \
  ESHU_7320_COST_PROOF=1 ESHU_7320_COST_PAIRS=9 \
  go test ./internal/storage/postgres -run TestSupersedePriorFailureClaimCost -count=1 -v
# 500 rows x 64 KB is the same test's second scenario; ESHU_7320_COST_ROWS,
# ESHU_7320_COST_DETAIL_BYTES, ESHU_7320_COST_STRESS_ROWS and
# ESHU_7320_COST_STRESS_BYTES change the cases.
```

```text
QUIET_HOST_TIMING_PLACEHOLDER (begin)
status: DONE. Quiet-host run, rule PD met in every run.
commit measured: c3febda3f2ea3760183392e4c22491b8f7067890 (base 8996bcc1e7,
  cumulative patch-id f556820f028925b1f19674644652e15163555aff), test binary
  precompiled before timing, TestSupersedePriorFailureClaimCost, 9 pairs, run twice
host: AWS EC2 r7a.4xlarge (AMD EPYC 9R14, 16 vCPU, 123 GiB), PostgreSQL 18.6
  postgres:18-alpine sha256:b07129cc272f, disposable container, autovacuum off,
  pg_stat_statements preloaded, other settings default (work_mem 4 MB)
load1 (1 s samples; PD limit 8 = half of 16 CPUs):
  set 1: start 1.35, end 1.07, max 1.35   set 2: start 0.77, end 1.41, max 1.56
PD limit: 8, applied from the raw one-second samples. The harness label at the
  measured commit used a fixed 9.0; the later harness fix changes the label only.
control canary: before text against itself, same alternation, every pair.
  Bound per case = Q3 + 3 x IQR of the 54 before-text runs of both sets; a
  pair is invalid if any of its before-text runs exceeds it. Valid pairs of
  18: 18, 15, 17, 14, 16, 11 (table order). No median moves more than ~1%
  with the invalid pairs included.
canary bound: derived from the two sets it judges, because no earlier quiet-host
  sets of this harness exist. Nothing is gated on it; with all 18 pairs the
  ratios are 1.008, 8.654, 1.255, 25.139, 1.224, 23.117.
seed: every swept row is a dead letter with details, the worst case. A row that
  never failed writes an empty object.

case (combined valid pairs)     before med  after med   ratio   delta     per row
projector claim 3,500 x 800 B   2.925313 s  2.948298 s  1.008   +23.0 ms  +6.6 us
projector claim 500 x 64 KB     0.123188 s  1.069122 s  8.679   +945.9 ms +1,892 us
reducer claim 3,500 x 800 B     0.106019 s  0.133611 s  1.260   +27.6 ms  +7.9 us
reducer claim 500 x 64 KB       0.039611 s  0.994843 s  25.115  +955.2 ms +1,910 us
reducer batch 3,500 x 800 B     0.110598 s  0.135354 s  1.224   +24.8 ms  +7.1 us
reducer batch 500 x 64 KB       0.042884 s  0.992119 s  23.135  +949.2 ms +1,898 us
same-pair ratio mean +- SD: 1.011 +- 0.017, 8.847 +- 0.526, 1.252 +- 0.025,
  25.529 +- 1.467, 1.231 +- 0.041, 23.020 +- 0.635
control pair ratio min..max (pooled): 0.986..1.015, 0.988..1.054,
  0.963..1.281, 0.763..1.167, 0.945..1.473, 0.897..1.745

exact seconds, before / after, pair order, set 1 then set 2:
projector 800 B before 2.915461 2.924018 2.971962 2.933188 2.935673 2.914932 2.937487 2.927399 2.935938
                        2.892016 2.910207 2.925068 2.925558 2.904318 2.928802 2.942172 2.906788 2.920893
                after  2.935876 2.980341 2.974783 2.949687 2.923755 2.948161 2.945892 3.151070 2.948927
                        2.926890 2.937745 2.918886 2.953161 2.925847 2.948435 2.971123 2.957558 2.939517
projector 64 KB before 0.132560 0.123159 0.125699 0.122518 0.122441 0.128265 0.123188 0.122750 0.122449
                        0.123895 0.123871 0.123628 0.125239 0.123064 0.123375 0.123541 0.122748 0.124080
                after  1.059185 1.065917 1.063672 1.064293 1.078494 1.066675 1.071471 1.079581 1.248490
                        1.074467 1.059709 1.063302 1.070810 1.069755 1.067572 1.241188 1.069122 1.067139
reducer 800 B   before 0.108714 0.108781 0.107520 0.108734 0.105487 0.108801 0.106486 0.104517 0.105507
                        0.106208 0.108939 0.105648 0.105758 0.105661 0.104467 0.105716 0.106019 0.106584
                after  0.134396 0.131631 0.134328 0.131321 0.132266 0.134341 0.134609 0.134004 0.129420
                        0.134009 0.132584 0.132056 0.132652 0.133611 0.130510 0.137345 0.137379 0.131292
reducer 64 KB   before 0.038823 0.040281 0.047337 0.039591 0.039594 0.039890 0.039228 0.039107 0.039074
                        0.039795 0.039631 0.039681 0.039723 0.039248 0.039003 0.039299 0.043107 0.040145
                after  1.003339 0.990674 1.018861 0.996709 0.997324 1.026637 0.990702 0.993072 1.000130
                        1.063569 0.994944 0.994742 1.178976 0.991862 0.997346 0.989140 0.982487 0.987957
batch 800 B     before 0.108374 0.112158 0.141610 0.111684 0.107545 0.106697 0.107404 0.108496 0.111576
                        0.112534 0.107818 0.112232 0.110745 0.110451 0.111263 0.107485 0.112046 0.107847
                after  0.137498 0.132749 0.134579 0.138060 0.131924 0.137372 0.134728 0.140438 0.132065
                        0.133433 0.137953 0.130239 0.135806 0.136297 0.136110 0.134902 0.133902 0.139653
batch 64 KB     before 0.043027 0.047496 0.045547 0.044682 0.047888 0.042358 0.043068 0.042511 0.041891
                        0.044396 0.042557 0.041888 0.043148 0.042884 0.042535 0.041957 0.041552 0.049131
                after  0.992119 0.989336 0.993903 1.001582 1.031465 0.985817 1.001401 0.987491 0.990174
                        0.987775 0.998872 0.996083 0.990710 0.987176 1.004668 0.998148 0.992075 1.031379

verdict against the 10% / 60 s stop threshold:
  projector claim 3,500 x 800 B: +0.8%, below 10%.
  reducer claim and batch claim 3,500 x 800 B: +26% and +22% of the statement
    (+27.6 and +24.8 ms for a 3,500-row sweep), above 10%, far below 60 s.
    Profiled: identical plan, no spill, same WAL record count, +986 B WAL
    and +0.14 dirtied pages per row; pg_stat_statements executor time per row
    26.4 to 34.4 us (claim) and 26.4 to 33.4 us (batch), matching the wall delta.
  64 KB, all three: x8.7 to x25, about +1.9 ms per superseded row, below 60 s.
  This is a measured cost: about 7 to 8 us per superseded row at 800 B, paid
  once per row, growing with the kept bytes.
QUIET_HOST_TIMING_PLACEHOLDER (end)
```

Not measured: concurrent claim traffic, table bloat and production autovacuum,
the Compose Postgres profile, 3,500 rows at 64 KB (about 6.7 s by extrapolation),
a work_mem between 64 kB and 4 MB, heap growth on a real table, and the built
binary (waived, below).

> Arbiter waiver, F1b (2026-09-28). The P5 requirement "on the built binary" is waived for #7320. The timing was taken with the package's claim harness at c3febda3f2, not with the reducer or ingester binary. Reasons: the only non-test code change is SQL text in three files; the harness runs the shipped statement constants, compiled into the test binary, with the arguments the queue methods pass; production and harness use the same pgx v5.9.2 database/sql driver and the same default exec mode, and production runs the claim through the same QueryContext call with no surrounding transaction; the harness schema is the bootstrap schema, triggers included; and pg_stat_statements executor time moves by the same amount as wall time (reducer claim 26.4 to 34.4 microseconds a row against +7.9 on the wall), so the cost is inside the statement, where a binary cannot change it. Not covered by this waiver and not measured: concurrent claim traffic, table bloat, production autovacuum, and the Compose Postgres profile. The waiver is void if any non-test file under go/internal/storage/postgres differs from c3febda3f2 at push.

## Observability

No-Observability-Change: `eshu_dp_superseded_generation_fence_total` and
`eshu_dp_poison_dead_letter_items` keep their meaning and labels. The new
evidence is row-level: `failure_details.prior_failure` on the superseded work
row, read with the recipe above. A supersede-over-a-failure counter was
rejected: 95.0% of ops-qa projector supersedes happen in the claim sweep, whose
result carries only the claimed row, so counting there means changing the claim
statement's result shape, and counting only the other writers would cover 5% and
mislead. The operator gap is at Fail time, not here (#7386).

## Ops-qa attribution of the 3,436 rows (issue item 2)

The rows cannot be recovered per row: class, message, details, status and the
generation's `failed` status are gone. Read-only ops-qa facts, measured
2026-09-28: 9,980 superseded projector rows, of which 9,483 (95.0%) came from the
claim sweep, 497 (5.0%) from Heartbeat and none from the Ack writers. The issue's
3,436 rows with `attempt_count = 3` split 3,317 claim sweep and 119 Heartbeat;
the 119 were claimed or running when superseded, so they were not dead-lettered
and the issue's inference over-counts by at least 119. The one surviving dead
letter is `graph_write_timeout`. Operator notes on 15 replayed projector rows in
`fact_replay_events` name a bare-label retract five-minute drain timeout, a
backend saturated by a retract storm and restart churn, and a NornicDB planner
fix: corroboration that the class was graph-write and retract timeouts, not proof
for 3,317 rows. Logs and Prometheus history for 09-18 to 09-24 are NOT_CHECKED.

## Follow-ups

- #7385: expose `prior_failure` in `get_generation_lifecycle`, the freshness CLI
  and the admin work-item listing (wire contract, OpenAPI lockstep).
- #7386: a dead-letter counter by `failure_class` at the projector Fail path;
  check reducer parity.
- #7387: `fact_replay_events.failure_class` is always NULL (the replay reads the
  row after clearing it).
- #7388: other overwrites of live-row failure fields:
  `projector_stale_scope_reclaim` and the operator note replacing
  `failure_details`.
- #7407: queue: `failure_details` has no size bound at the Fail-time writer;
  measure the real widths on ops-qa first, and put any bound at the writer.
- #7408: projector: the claim sweep costs 0.84 ms per superseded row, 28 times the
  reducer sweep; rank it by measurement before any work.

## Not done, on purpose

Option B (new columns): `ADD COLUMN` takes ACCESS EXCLUSIVE on the claim table
for evidence no reader needs indexed. Leaving `failure_class` unchanged: it
breaks the pinned marker and makes a retired generation read as a live failure.
An audit table written from the sweep, casting the old details to jsonb,
truncating, a claim result-shape change, an observer gauge over superseded rows,
listing superseded rows as dead letters, backfilling old rows, and any
serialization.
