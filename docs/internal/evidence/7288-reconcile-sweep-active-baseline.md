# Reconcile Sweep Stacking And Active-Only Delta Baseline (#7288, #7317)

This note records the proof for two fixes that ship together:

- **Fix A' (#7288).** The git reconciliation sweep no longer forces a new full
  snapshot while the scope's previous full generation is still in flight, and it
  backs off for `Interval / 4` after a full generation that failed or was
  superseded before it activated.
- **Fix B (#7317).** The delta baseline is the commit of the scope's `active`
  generation. A generation superseded while still pending is no longer treated
  as projected.

B must not merge without A': on its own it would keep a scope due through every
never-activated full generation, and the sweep would stack without bound apart
from `ESHU_REPO_RECONCILE_MAX_PER_CYCLE`.

Classification: correctness win (delta baseline, sweep obligation) and
scheduling win (no redundant same-commit full snapshots while the projector is
behind). No end-to-end wall-clock claim is made.

## Defect

Both reads lived in `go/internal/storage/postgres/generation_projected_commit.go`
and counted `status IN ('active', 'completed', 'superseded')` with no
`activated_at` check.

- The sweep's read ignored `pending`, so a forced full generation that was still
  queued or running left its scope overdue. Each selection cycle (about six
  minutes on the QA environment) forced another full snapshot of the same commit, and the
  projector claim superseded the older ones before they activated. A superseded
  generation then satisfied the sweep for a full interval although it never
  reached the graph.
- The baseline read accepted a generation superseded while pending. A delta
  diffed against that commit skipped every change between the active commit and
  it until the next activated full reconciliation.

A read-only QA review on 2026-09-27 confined the stacking to 2026-09-18
through 2026-09-24, while the projector ran behind the selection cycle. From
2026-09-25 the fleet writes about one reconcile full per scope per day and
nearly all activate. That window also holds 3,028 reconcile fulls that made
their attempts in about 23 minutes and were superseded about 16 hours later.
Reading them as dead-lettered after exhausting retries is an inference: the
claim-path supersede overwrote their failure class, so the cause is not
observable here and is tracked separately.

## Fix

- `lastProjectedCommitSHAQuery` reads `status = 'active' AND activated_at IS
  NOT NULL` through `scope_generations_active_scope_idx`. A scope with no active
  generation returns no baseline, and the sync takes a full snapshot.
- `fullReconcileStateQuery` replaces `lastFullProjectionAtQuery`. It returns the
  newest activated full generation and the newest full generation of any status.
  `IngestionStore.LastFullProjectionAt` is removed; the resolver method is
  `FullReconcileState`, returning `scope.FullReconcileState`.
- `reconcilePolicy.decide` is pure. Its reasons are the closed set `fresh`,
  `reconcile_in_flight`, `in_flight_expired`, `reconcile_retry_backoff`,
  `retry_after_unprojected`, `never_reconciled`, and `interval_elapsed`. The
  backoff is derived from the interval; no environment variable is added.

## Proof

All live runs used a disposable `postgres:18-alpine` (PostgreSQL 18.6)
container through `ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN`.

### RED on the pre-fix code

Fix B, `TestLastProjectedCommitSHAUsesActiveGenerationLive` against the old
query:

```text
--- FAIL: .../superseded_while_pending_does_not_become_the_baseline
    LastProjectedCommitSHA() = "sha-b", want "sha-a"
--- PASS: .../active_delta_generation_is_the_baseline
--- FAIL: .../no_active_generation_falls_back_to_a_full_snapshot
    LastProjectedCommitSHA() = "sha-old", want ""
--- PASS: .../only_a_pending_generation_has_no_baseline
```

Fix A', storage read. A throwaway probe seeded one activated full 48 hours old
plus a newer full, and read the old `LastFullProjectionAt`:

```text
pending_30m: main reports last full 48h0m0s old => sweep forces a new full despite latest full "pending"
failed_1h: main reports last full 48h0m0s old => sweep forces a new full despite latest full "failed"
superseded_unactivated_1h: last projected full = <1h ago>, want <48h ago> (the only activated full)
```

Fix A', collector. The multi-cycle tests ran against the old resolver
interface, with a stateful fake that mirrors the old status list:

```text
TestReconcileSweepForcesOnceWhilePreviousFullPending: forced reconciles across 5 cycles = 5, want exactly 1
TestReconcileSweepBacksOffAfterFailedFull: forced reconciles inside the Interval/4 backoff = 59, want 0
TestReconcileSweepControlProjectedFullNotForced: PASS
```

### GREEN

- `TestLastProjectedCommitSHAUsesActiveGenerationLive`: all four cases pass.
- `TestFullReconcileStateLive`: latest full projected; pending at 30 minutes;
  pending at 25 hours; failed at 1 hour; failed at 7 hours; superseded before
  activation at 1 hour; a pending delta newer than the projected full, which is
  ignored; and a scope with no generation.
- `TestReconcilePolicyDecide`: every reason, with boundaries at exactly
  `Interval`, exactly `Interval / 4`, one nanosecond inside each, and a future
  (clock-skewed) ingest time.
- `TestReconcileSweepForcesOnceWhilePreviousFullPending`: exactly one forced
  reconcile over 5 cycles 6 minutes apart.
- `TestReconcileSweepBacksOffAfterFailedFull`: none inside 6 hours, then
  exactly one.
- `TestReconcileSweepDeltaSupersedesPendingFull`: the delta taken while the full
  is pending diffs against the active commit; after the delta supersedes the
  full and activates, the full is retried exactly once after the backoff.
- `TestReconcileSweepTelemetry`: forced counts by `reason`, the suppression
  counter, and the INFO and WARN `git_reconcile_forced` logs.

## Performance Evidence

Performance Evidence: baseline is origin/main `98394122fa`; the after
measurement is this branch. Backend PostgreSQL 18.6 (`postgres:18-alpine`).
Both reads run once per git scope per selection cycle while the per-cycle
reconcile budget remains. The generic-plan shim used the `scope_generations`
DDL and indexes from migration `002_scope_generations.sql`, seeded with 800
scopes of 64 generations (one scope carries 2,000 more), then `ANALYZE`. Every
query was run as `PREPARE` and `EXPLAIN (ANALYZE, BUFFERS) EXECUTE` under
`plan_cache_mode = force_generic_plan`, across four scopes including a hot scope
and a missing one. The query text was extracted from the Go constants, not
hand-copied. Each figure is one run, so treat it as plausibility, not a latency
distribution.

| query | plan (generic, `Index Cond: scope_id = $1`) | execution | buffers |
| --- | --- | ---: | ---: |
| old baseline | `scope_generations_scope_latest_lookup_idx` | 0.008-0.015 ms | 3-4 |
| new baseline | `scope_generations_active_scope_idx` | 0.011-0.057 ms | 2-3 |
| old sweep read | `scope_generations_scope_latest_lookup_idx` | 0.008-0.018 ms | 3-7 |
| new sweep read | same index, two probes | 0.024-0.132 ms | 6-18 |

Worst case: a scope with 3,000 never-activated full generations newer than its
only activated full. The projected-full probe filtered 3,000 rows on the same
index scan: 76 buffers and 0.34 ms. No plan in any case was a sequential scan
or a sort.

## No-Regression Evidence

No-Regression Evidence: focused `go test` and `go test -race` pass on the
collector, storage, scope, and telemetry packages; the plan table above shows
no scan or sort regression. The QA before/after comparison is a no-regression check only. Stacking has
been near zero since 2026-09-25, so it cannot show a reduction, and projector lag
must not be induced there to demonstrate one. After deploy, over a window of at
least 48 hours, the acceptance is:

- full generations per day within 5% of the scope count;
- no scope with two or more never-activated fulls inside any 6-hour window;
- no activated delta whose baseline was a never-activated generation;
- no scope past 30 hours without a projected full, other than scopes with a
  dead letter.

Capture the per-day table and the count of scopes past 30 hours before deploy.
Re-run the old-rule versus new-rule due-scope comparison immediately before
deploying to rule out a stampede.

## Observability Evidence

Observability Evidence: `TestReconcileSweepTelemetry` asserts the counters and
logs below on the production sync path.

- `eshu_dp_collector_reconciliation_full_snapshots_total` gains the bounded
  `reason` label.
- `eshu_dp_collector_reconciliation_suppressed_total` is new, labeled by
  `reason` (`reconcile_in_flight` or `reconcile_retry_backoff`).
- The `git_reconcile_forced` log carries `scope_id`, `reason`,
  `last_projected_full_at`, `latest_full_at`, and `latest_full_status`. It is
  WARN for `in_flight_expired` and `retry_after_unprojected`.
- `scope_id` is on the log only, never a metric label.

## Known Limits

- The baseline read is not fenced against activation. A pending generation can
  activate between the baseline read and the delta's projection; this race
  exists on main and is not widened here. It is tracked as the delta-baseline
  fence follow-up.
- The native and webhook selectors can both evaluate the same scope. If they
  run concurrently, each can force one full, which bounds the duplicate at one
  per selector per backoff window. Whether they run concurrently is NOT_CHECKED.
- `reconcileBudgetRemaining` short-circuits before the decision, so a due scope
  that is out of budget produces no signal that cycle.
