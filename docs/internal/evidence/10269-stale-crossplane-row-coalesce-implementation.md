# #10269: Coalesce Stale Per-Generation Crossplane Rows — Implementation

Implements the prune-in-sweep design from
`10269-stale-crossplane-row-coalesce-design.md` on top of merged #10106
(`0228c689f4`, verified ancestor of the rebase base `6b66e6b23e`).

## What changed

One statement shape, in `reducer_generation_filter_sql.go`, shared by both
claim statements through `supersedeInactiveReducerGenerationsCTE`:

- The #10106 blanket exemption
  (`stale.domain <> 'crossplane_satisfied_by_materialization'`) is gone
  from the sweep predicate. Every stale candidate now enters the single
  ordered lock step (`FOR NO KEY UPDATE`, `work_item_id COLLATE "C"`),
  Crossplane rows included. An added carrier arm in the same scan also
  locks a pending/retrying Crossplane row on the scope's **active**
  generation when an older stale sibling is queued.
- The lock CTE now also emits `scope_id`, `domain`, `status`,
  `generation_id`, and the row generation's `ingested_at`. A materialized
  `superseded_crossplane_newest_carrier` CTE derives the newest locked
  pending/retrying Crossplane carrier per scope from that output.
- The UPDATE keeps its original drive shape (locked ids by primary key,
  stage/status recheck) and adds: a generation recheck
  (`generation_id <> active_generation_id`, so locked active-generation
  carriers are never superseded) and, for Crossplane rows only, the
  carrier condition — the scope's newest locked carrier must be strictly
  newer in activation order (`ingested_at`, then `generation_id`).

Semantics preserved from the design: group is (scope, domain); carrier set
is pending/retrying only (claimed/running never absorb, failed/dead_letter
newest absorbs nothing); sole stale row survives and rebinds (#10106);
prior-failure fold, readiness OR-gate, $2 domain filter, and fan-out debt
spare are untouched; lock order and worker/batch/lease settings unchanged.

## Why not the design sketch's snapshot EXISTS (the EvalPlanQual finding)

The design's NOT_CHECKED section flagged the key unknown: whether the
lock step's `FOR NO KEY UPDATE` EvalPlanQual recheck refreshes a
snapshot EXISTS over the live table. The live interleave test answered
**no**: with the sketch's conditional EXISTS, a carrier claimed (committed)
while the sweep waited on the stale row's lock was still seen as pending,
and the sole fallback row was superseded — destroying the #10106 rebind
fallback the carrier-set conservatism exists to preserve. `FOR KEY SHARE`
on the sibling (in the lock step or in an UPDATE-phase recheck) failed the
same way: a key-share lock does not conflict with a non-key UPDATE, so it
never triggers chain-following re-evaluation.

Locking the carriers in the same ordered pass is the race-free form of the
same check: membership in the lock step's output is decided by row-self
status quals, which EvalPlanQual *does* re-evaluate on the newest row
version. A sibling claimed between snapshot and lock never enters the
locked set, so the stale row survives. This was verified live before
adopting it (the sketch form failed the new test; the locked-set form
passes).

## Proof summary (live, disposable Postgres 18)

New `stale_coalesce` subtests under the scheduled
`TestCrossplaneSatisfiedByRedriveClosesXRDLagWindowLive`
(`satisfied_by_stale_coalesce_test.go`), all through the real
`ReducerQueue` and reducer `Runtime`:

- Seven shim guard cases in one drain: sole stale row executes;
  dead-letter newest and failed newest do not absorb an older pending
  row; stale failed row prunes against a newer pending carrier with the
  `prior_failure` fold intact; retrying is a carrier; cross-scope
  isolation (sole scope unaffected by other scopes' newer generations);
  every surviving execution writes its edge under the active (delta)
  generation.
- Three queued generations collapse in one **batch** claim (only the
  newest carrier is claimed; the rest are superseded).
- Active-generation carrier: non-quiet delta row carries two stale
  siblings; one execution; carrier itself never superseded.
- In-flight overlap bound: a row claimed and executed before newer
  generations queue adds exactly one extra execution (2 total).
- EvalPlanQual interleave: holder locks the stale row, sweep blocks on
  it (proven via `pg_blocking_pids`), carrier is claimed and committed
  by a third session, holder commits — stale row is **not** superseded.
- Concurrent claimers on independent connections over a 3-generation
  backlog: exactly one winner, the newest carrier, one edge.
- Backlog counts: 3 scopes x 3 generations = 9 rows drain to 3 handler
  executions and 6 superseded rows (no-coalesce: 9 executions).

Regression runs: full redrive live suite (all subtests incl.
`quiet_delta_replay`), the #8205 sweep lock-order live proofs (one-way
blocking both orders, held-row race, 150 concurrent claim/replay rounds
with 0 x 40P01 and exact accounting, active-generation guard, sweep EPQ
drop case), and the full `storage/postgres` hermetic package (one
pre-existing environment failure: `TestSchemaBootstrapCoordinationDefaultsFitTheJobDeadline`
needs `ESHU_DEPLOY_CHECKOUT`, also fails on the untouched base).

RED exits before the change: hermetic shape pins exit 1; live chain
subtest exit 1 (oldest generation claimed instead of coalescing). GREEN
after: all of the above exit 0.

Performance Evidence: execution/row counts above, plus an
`EXPLAIN (ANALYZE, BUFFERS)` differential of the single claim statement
on a backlog fixture (200 scopes; 2,000 stale workload rows + 600 stale
Crossplane rows), base vs this change, same disposable Postgres, host
load high so timings are indicative only — plan shape and buffers are
the evidence: lock step stays one sorted `LockRows` pass (2,000 rows
base / 2,600 after, the +600 being the newly lockable Crossplane rows);
the UPDATE stays primary-key driven per locked row (superseded 2,000
base / 2,400 after = +400 pruned Crossplane rows, buffers 45,411 /
53,724 shared hits, i.e. flat per superseded row); the candidate
anti-join over the superseded set stays a Hash Anti Join in both. The
carrier-arm EXISTS never executed for non-Crossplane rows in the probe
plan (short-circuited join filter). Two plan regressions found and fixed
during the probe: an UPDATE-side locked-set join flipped the candidate
anti-join to a nested loop (reverted to the original `ANY(ARRAY(...))`
drive), and the carrier CTE needed `AS MATERIALIZED` (inlined form
re-ran per row).

Observability Evidence: no new metrics or labels. Pruned rows land in
the existing `superseded` state with
`failure_class = reducer_superseded_by_newer_active_generation` and
`domain`/`scope_id` in `failure_details`, countable per scope/domain as
before. Removing redundant runs also stops
`eshu_dp_crossplane_satisfied_by_edges_total` from counting the same
scope's edges once per queued generation per drain epoch.

## NOT_CHECKED

- Batch-claim EXPLAIN (the batch statement embeds the identical sweep
  CTE; covered by shape pins for both statements and the batch live
  subtest, but no separate batch plan was captured).
- Quiet-host wall-time comparison (host load average 3-37 during the
  probes; counts and buffers above are the claim, no latency bound).
- Production backlog shape for this domain (fixture counts only, as in
  the design note).
- `BenchmarkReducerQueueClaimDeepQueue` before/after (superseded by the
  EXPLAIN differential for this change; not run).
- Deadlock interaction between the new active-generation carrier locks
  and operator replays over Crossplane rows specifically: the #8205 live
  proofs (workload domain) pass unchanged and carrier rows lock in the
  same single ordered pass, but no replay-vs-sweep race was seeded with
  Crossplane carrier rows as the contested set.
