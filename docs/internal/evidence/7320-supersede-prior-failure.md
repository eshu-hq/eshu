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
- No truncation. Truncated evidence is wrong evidence; the cost is measured
  below instead.
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

Classification: correctness win. The fold adds bytes to every superseded row
(next section, Performance); its wall-time cost is not yet measured on a quiet
host. The projector claim sweep is a separate long pole of its own, about 2 to
3.5 s for 3,500 superseded rows on the loaded host, most of it work this change
does not touch.

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

The existing #7108, #7115 and #7130 live suites pass at dbe784a0bd (rebased onto
origin/main c96a8ffc28): the `TestSupersede`, `TestPriorFailure`, `TestProjector`
and `TestReducerQueue` suites with both live DSNs set on a throwaway PostgreSQL
18.6: 348 passed, 0 failed, 13 skipped (other DSNs; the cost and memory
harnesses are opt-in). Later commits on the branch change documentation only.

## Performance

Performance Evidence: what is proven is the plan, the bytes and pages the fold
writes, and the memory it holds, all counts that do not depend on host load.
What is NOT yet proven is wall time: the timing proof on a quiet host has NOT
BEEN DONE, and this note makes no no-regression claim for wall time. Every wall
time taken so far ran on a loaded shared host (load average 13 to 55) and is
NON-PD (the owner's timing-proof rule: load1 below 9 at start, end and maximum,
with a control canary), so none of it is quoted as a measurement below.

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
text is simply 80 times larger. This is far above any details size seen (the one
ops-qa dead letter has 793-byte details) and is the reason the bound is measured and not enforced by
truncation.

### Why the projector fold cost differed from the reducer's in the first run

An earlier loaded-host run showed the fold adding about 0.14 ms a row to the
projector claim (0.485 s over 3,500 rows) and about 8 microseconds a row to the
reducer claim, a 17-fold gap that nothing in the plans explained. The theory
"the projector statement does 17 times more work per superseded row" was tested
before this prose was written, and it does not hold:

- The deterministic table above shows the same +986 bytes and +0.14 to +0.15
  dirtied pages a row for both statements, and +0.28 against +0.14 shared hits a
  row. There is no 17-fold difference in work.
- A throwaway shim (not committed) ran the two claim statements at 3,500 x
  800 B, nine before/after pairs with alternating first mover, and read the
  backend's on-CPU time from `/proc/PID/schedstat` in the container, which is
  far less sensitive to host load than wall time (host load about 15). Medians:
  projector claim 2.1759 s before, 2.2161 s after (+0.040 s, 11.5 microseconds a
  row); reducer claim 0.1164 s before, 0.1282 s after (+0.012 s, 3.4 microseconds
  a row). That is a factor of 3.4, not 17, and the projector's own per-run
  spread (2.09 to 2.35 s before) is larger than its delta, so the delta is not
  resolved either.
- A partial loaded-host run of the shipped harness (12 pairs, load 13 to 50; it
  ended on a dropped connection after the projector and reducer cases) gave a
  projector median ratio of 0.991 with a control pair-ratio spread of
  0.69 to 1.05: the earlier +0.485 s was not reproduced.

The reading: the 0.14 ms a row was host-load noise, not the fold. The remaining
3.4-fold difference between the two statements is consistent with the projector
sweep holding the wider rows in a hash node (705 kB to 3,680 kB, next section)
where the reducer sweep holds none, but that mechanism is a hypothesis, not
something this run isolates. The quiet-host run below settles the absolute
figure.

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

### Wall time: NOT YET DONE

The cost is paid once per superseded row, because supersede is terminal, and
the work per row is the bytes above. Whether that is a wall-time regression on
the claim sweep is not established: the loaded-host samples spread over more
than the effect (projector claim medians of pairs at 3,500 x 800 B ranged from
x0.99 to x1.14 between runs, with a control pair-ratio spread of 0.69 to 1.05),
so they support neither "no regression" nor a bound. The 64 KB stress case is
expected to be visibly slower (about 8 more pages and 70 KB more WAL a row) and
is accepted as a cost of not truncating; it is not a supported size.

The quiet-host run is one command per case, 9 or more before/after pairs,
interleaved with alternating first mover, each statement with a same-text
control, the run labelled PD only if load1 stayed below 9 at start, end and
maximum:

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
status: PENDING. The quiet-host run has not been done; nothing below is measured.
host / load1 start, end, max / control canary:  <to fill>
commit measured:                                <to fill>
projector claim 3,500 x 800 B: before median <s>, after median <s>, ratio <x>, exact seconds per pair <list>
projector claim 500 x 64 KB:   before median <s>, after median <s>, ratio <x>, exact seconds per pair <list>
reducer claim, batch claim:    same two cases <to fill>
verdict against the 10% / 60 s stop threshold:  <to fill>
QUIET_HOST_TIMING_PLACEHOLDER (end)
```

Not measured: a host with the production autovacuum and TOAST history, and the
built binary (the harness runs the shipped statement text with the queue
methods' arguments through `database/sql`; the text is identical, but this is
not the binary).

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

## Not done, on purpose

Option B (new columns): `ADD COLUMN` takes ACCESS EXCLUSIVE on the claim table
for evidence no reader needs indexed. Leaving `failure_class` unchanged: it
breaks the pinned marker and makes a retired generation read as a live failure.
An audit table written from the sweep, casting the old details to jsonb,
truncating, a claim result-shape change, an observer gauge over superseded rows,
listing superseded rows as dead letters, backfilling old rows, and any
serialization.
