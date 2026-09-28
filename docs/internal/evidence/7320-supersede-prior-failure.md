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

Classification: correctness win. Next measured long pole: the projector claim
sweep itself, about 2.4 s for 3,500 superseded rows (0.68 ms a row), unchanged by
this fold and not part of this issue.

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
  fails if it finds fewer than the five known writers. Seeded violation: a
  planted writer without the fold, and the real `projector_queue_sql.go` with
  the fragment stripped, are reported; the clean tree passes. Cutting the
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

The existing #7108, #7115 and #7130 live suites, and the rest of
`go/internal/storage/postgres`, pass on the final commit.

## Performance

No-Regression Evidence: PostgreSQL 18.6, worst case of 3,500 dead-lettered rows
with 800-byte details, each on its own scope, all superseded by one claim
(ops-qa has one dead letter with 793-byte details and 122,734 work rows), plus a
stress case of 500 rows with 64 KB details. Plan first (`EXPLAIN (VERBOSE,
COSTS)` of the projector claim with and without the fold, 320 lines each):
identical node types and order; the total cost estimate moves 181814.66 to
181855.66 (+0.02%) and the sweep's row width from 52 to 904 bytes, the old
failure columns the target list now carries. `EXPLAIN (ANALYZE, BUFFERS)` shows
no spill for any statement in either case.

Then the shipped claim statements, executed with the arguments their queue
methods pass, before (the shipped text with the fold cut out, derived by the
test from the constant) against after, interleaved with alternating first mover,
nine pairs, freshly seeded each run, medians. Two full runs on a loaded host:
the first drove the queue methods with the statement text swapped in a package
variable (an earlier harness, dropped so production SQL stays constant), the
second is the committed harness and runs both variants as plain statements.

| Statement | Case | Before | After | First run |
| --- | --- | --- | --- | --- |
| projector claim | 3,500 x 800 B | 3.478298 s | 3.963535 s (x1.140) | x1.013 |
| projector claim | 500 x 64 KB | 0.076079 s | 0.165747 s | 0.078 to 0.188 s |
| reducer claim | 3,500 x 800 B | 0.142681 s | 0.145012 s (x1.016) | x1.233 |
| reducer claim | 500 x 64 KB | 0.017419 s | 0.144022 s | 0.019 to 0.136 s |
| reducer batch claim | 3,500 x 800 B | 0.107870 s | 0.113706 s (x1.054) | x1.216 |
| reducer batch claim | 500 x 64 KB | 0.017235 s | 0.112008 s | 0.021 to 0.138 s |

The ratio at the ops-qa scale moves between runs (projector x1.01 to x1.14,
reducer x1.02 to x1.23) because of host load; read it as "at most about 15% of a
sweep that runs once per superseded row", not as a precise figure.

Memory and temp files (`TestSupersedePriorFailureClaimMemory`,
`EXPLAIN (ANALYZE, BUFFERS, VERBOSE)` in a rolled-back transaction, per-node
`Memory Usage`, `Batches`, `Disk` and temp blocks summed over the plan). The
claim statements set no `work_mem` of their own (no `SET LOCAL`), so the server
default of 4 MB applies, with the default `hash_mem_multiplier` of 2 for hash
nodes; a second run at the PostgreSQL minimum of 64 kB shows how close each
variant sits to spilling.

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
batches, and the temp block count at 3,500 rows goes from 4,773,360 to 6,819,621
(+43%) with the wider rows. The reducer statements do not hold the failure
columns in any node. Not measured: backend RSS, and a `work_mem` between 64 kB
and 4 MB.

These runs are NON-PD: the host was shared and loaded (load average 24 to 55
during the runs), so wall times are indicative only and a quiet-host re-run of
`TestSupersedePriorFailureClaimCost` is the timing evidence. The cost is paid
once per superseded row, because supersede is terminal. At the ops-qa scale it is
0.002 to 0.03 s for 3,500 reducer rows (up to about 8 microseconds a row) and
up to about 0.5 s on a 3.5 to 4 s projector sweep of the same rows, whose own
per-row work dominates. The 64 KB case is far above any observed details size
and costs about 0.22 ms a row (jsonb build and text output of the old value); it
is the reason the bound is measured and not enforced by truncation. Not
measured: a host with the production autovacuum and TOAST history.

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
