# Deployed-binary contention workload spec (`#8875`, `#9727`)

Status: **WRITTEN SPEC ONLY — NOT EXECUTED**. Nothing here has run. The scoped
synthetic-write grant is recorded (`#8875` comment `6558745112`, arbiter
ruling 2026-11-01). Execution stays HELD on its open preconditions: the
enablement IaC PR and its pre-authored revert PR are not yet linked on
`#8875` (the grant requires both numbers before the first write), and the
`#9727` authority decision for enabling claim flow is not recorded. Companion
to `REMOVAL_PREP.md` §3 (lock audit and existing evidence) and
`PLANE_DECLARATION_9376.md` §4-§5. Issue `#9727` cites "§4" for this
workload; §4 is the throughput procedure, and contention is §3 plus this
file.

What the run can support: the **claim-plane** under contention on the
deployed binary. Core does not consume the pin on `origin/main`
(`PLANE_DECLARATION_9376.md` §4, ABSENT-on-main), so the run cannot prove
generation-level fencing. The pin step (§4, step 7) proves the satellite
route's single-holder and stale-rejection behavior only.

## 1. Grant bounds (read first; anything outside is a stop)

From the grant comment, 2026-11-01:

**Granted**
- Synthetic writes confined to the **satellite's own database** (claims,
  items, runs, pins). Row ids use the prefix `parity-%` or
  `synth-8875-rehearsal-*`. This spec uses only `synth-8875-rehearsal-`.
- The **disposable in-tree leg database**: a throwaway database that the
  harness creates and force-drops itself
  (`postgresproof.OpenDisposableBootstrapDatabase`, called from
  `openInTreeBackend` in `go/internal/workflow/parity/backend.go`). It runs
  on a **non-core Postgres server** (a local container). `PARITY_INTREE_DSN`
  is that server's administrative DSN naming the `postgres` database, and
  `PARITY_INTREE_DISPOSABLE=1` opts in. The executing lane records the exact
  generated database name before the first write. `PARITY_INTREE_DSN` must
  not point at the core QA-environment server or database; the in-tree leg
  never touches core data.
- Claim-flow enablement on the **enumerated synthetic instances only**,
  through a reviewed IaC PR with a pre-authored revert PR (the `#448`/`#449`
  pattern). The enablement PR number and the revert PR number are linked on
  `#8875` before the first write; until both are linked this spec is not
  executable. The dark re-pin is the separate PR `#491`.

**Not granted**
- Any write to the core QA-environment database: fact tables, core workflow or claim
  tables, migrations, DDL.
- Claims on any real scope.
- Any other environment.

Rules that follow:
- Every selector, instance, and id the run uses is under the single prefix
  `synth-8875-rehearsal-`. The parity prefix `parity-%` belongs to the
  harness and the rehearsal lane; this run neither creates nor deletes it.
- Do **not** call the harness satellite reset. It issues a table-wide
  `TRUNCATE` of the satellite tables (`truncateSatelliteTables` in
  `go/internal/workflow/parity/fixtures.go`), which would erase rehearsal-lane
  rows. Cleanup is by prefix filter only (section 8).
- Pre-run and post-run snapshots (section 5) cover every satellite table the
  run can touch, so a non-synthetic change is visible.
- The grant expires at acceptance recording or at revert, whichever comes
  first. A run after expiry is out of grant.

## 2. Inputs and bounds the lane must record before the first write

Each row is either a concrete value or a field the executing lane fills in
**before the first write**. A blank field stops the run; do not guess. Section 6
and section 7 refer to these rows by name.

| Field | Value or source |
| --- | --- |
| `digest_under_test` | `ghcr.io/eshu-hq/eshu-workflow@sha256:db5a3a53a2bc8c5d9279403d95179950b13069a7c82b9568ff6a3f0e36e86dbc` (source `33f48f4a`), or the digest bound by the parity re-bind if `main` moved |
| `iac_enable_pr`, `iac_revert_pr` | numbers linked on `#8875`; set by the executing lane |
| `authority_decision` | link to the `#9727` decision; set by the executing lane |
| `intree_database_name` | generated disposable database name; set at run start |
| `worker_counts` | production worker count per claim-capable kind, read-only from deployed config; run at 1x and 2x of the largest |
| `lease_ttl`, `heartbeat_interval`, `reap_interval` | read from `ESHU_WORKFLOW_COORDINATOR_CLAIM_LEASE_TTL`, `..._HEARTBEAT_INTERVAL`, `..._REAP_INTERVAL` ([`config.go:123`](https://github.com/eshu-hq/eshu/blob/15caae83c5cd1595480c20173bab20f1f65614f7/go/internal/coordinator/config.go#L123)) |
| `backlog_items` | peak nonterminal `workflow_work_items` depth from production history; if history is smaller, 100,000 synthetic items across at least 200 synthetic scopes |
| `quota_budget` | per-leg budget (in-tree: per (component, scope) hour bucket; satellite: per-kind hourly quota flag of `claim-plane`); at least one scope at budget and one below |
| `window_seconds` | run duration per seed and leg; set by the executing lane, with a hard wall-clock cap recorded as `wall_clock_cap` |
| `starvation_bound_seconds` | set by the executing lane from the baseline p99 claim wait (suggested start: 3x); recorded before the run |
| `lock_wait_bound_ms` | set by the executing lane from the baseline max lock wait |
| `pg_headroom` | satellite database CPU, connection count, and replication-lag limits (each a number with a unit), set by the executing lane for the designated cluster |
| `baseline_throughput` | 30-claim+complete probe loopback datum, 892.8 in-tree / 894.5 satellite ops/s; informational, contention gates on correctness |

## 3. Per-leg applicability

The harness backends differ, so one mix does not run unchanged on both legs
(`go/internal/workflow/parity/backend.go`: the in-tree store has no commit-pin
column, and only the in-tree store implements
`CreateRunWithWorkItemsIfNoOpenTargets`; the satellite refuses a client-side
shim, #9428 ruling, and its server exposes 20 routes with no such route).

| Step | In-tree leg (disposable database) | Satellite leg | Pass criteria that apply |
| --- | --- | --- | --- |
| 1 claim | applies | applies | exactly-once, fencing, starvation |
| 2 heartbeat | applies | applies | single-holder, fencing |
| 3 complete / release / fail | applies | applies | exactly-once, zero loss |
| 4 reap | applies | applies | reclaim, stale-fence rejection |
| 5 quota | applies, per (component, scope) bucket | applies, per-kind hourly quota | no overshoot, judged per leg against that leg's own budget granularity |
| 6 advisory (guarded admission) | applies | **not applicable**: no route, no shim; record `ErrGuardedAdmissionNotSupported` | zero deadlocks, no duplicate runs (in-tree only) |
| 7 pin | **not applicable**: no commit-pin column; record `ErrPinNotSupported` | applies | stale-fence rejection for the pin, single-holder (satellite only) |
| 8 replay | applies (`claim_replay_ack.go`) | applies; the observed code is recorded, and a code that differs from the in-tree leg is a logged divergence, not a silent pass | replay idempotence |
| 9 stale writers | applies | applies | stale-fence rejection |

A step marked not applicable is recorded as `NOT_APPLICABLE` with the
sentinel code, never as `PASS`. Criteria that rest only on an inapplicable
step (advisory duplicates, pin rejection) are judged on the leg that has the
step and are not claimed for the other. The satellite leg gives no advisory
evidence, and the in-tree leg gives no pin evidence; the report says so.

## 4. Synthetic scope, IDs, and isolation

Every row the run writes carries the prefix `synth-8875-rehearsal-`:

- `run_id`: `synth-8875-rehearsal-run-<seed>-<n>`
- `work_item_id`: `synth-8875-rehearsal-wi-<seed>-<n>`
- `scope_id`: `synth-8875-rehearsal-scope-<k>`
- collector instance: `synth-8875-rehearsal-instance-1` on an enumerated
  synthetic instance with an existing claim-capable kind. On the in-tree leg
  the advisory lock key derives from kind + instance
  ([`workflow_control_open_targets.go:28`](https://github.com/eshu-hq/eshu/blob/15caae83c5cd1595480c20173bab20f1f65614f7/go/internal/storage/postgres/workflow_control_open_targets.go#L28)),
  so the synthetic instance keeps the lock keyspace apart from real work.
- owner ids: `synth-8875-rehearsal-owner-<w>`, one per worker goroutine.

The seed is fixed and recorded; ids are deterministic so a rerun produces the
same rows. The run uses a selector that matches only the synthetic instance,
so no real collector can claim a synthetic item and the synthetic workers
cannot claim a real one. The lane proves this first with a dry claim that must
return idle while real work exists.

## 5. Workload (interleaved, at production concurrency)

Run a single timed window per seed and per leg, one leg at a time, same seed.
Each worker loops over a weighted mix so every operation overlaps every other
on the same rows. Seeds 1 and 2 are mandatory; add seeds until two
consecutive seeds are clean. Steps follow section 3 for applicability.

1. **Claim**: `ClaimNextEligible` from W claimers (W = 1x, then 2x
   `worker_counts`) against `backlog_items`.
2. **Heartbeat**: each live claim heartbeats at `heartbeat_interval`.
3. **Complete / release / fail**: a seeded share of claims ends by
   `CompleteClaim` (60%), `ReleaseClaim` (15%), `FailClaimRetryable` (15%),
   `FailClaimTerminal` (10%).
4. **Reap**: `ReapExpiredClaims` runs at `reap_interval` while a seeded 5%
   of workers skip a heartbeat so their lease expires mid-flight.
5. **Quota**: at least one synthetic scope is held at its budget so the
   reservation path runs under claimant overload.
6. **Advisory** (in-tree leg only): concurrent
   `CreateRunWithWorkItemsIfNoOpenTargets` calls for the same (kind,
   instance) and open targets, from at least 25 goroutines per round.
7. **Pin** (satellite leg only): a seeded share of claims calls
   `ValidateAndPinClaimForCommit` before completing; some are pinned and then
   outlive the commit deadline; some are pinned after reclaim (stale).
8. **Replay**: a seeded share of terminal calls is replayed once after an
   injected timeout.
9. **Stale writers**: a seeded share of workers keeps writing with an old
   fencing token after their claim was reaped and re-claimed.

Snapshots: before step 1, record row counts and counters for every satellite
table the run can touch (and the in-tree disposable database's tables), split into
prefixed and non-prefixed rows. After cleanup, diff.

## 6. Pass criteria (all must hold on every leg where the step applies)

- **Exactly-once**: every synthetic work item reaches exactly one terminal
  outcome; none is completed twice; no item has two live claims at any sampled
  instant (sample at least once per second).
- **Fencing monotonicity**: per work item, successive claim fencing tokens
  strictly increase; `UNIQUE (work_item_id, fencing_token)` is never violated.
- **Stale-fence rejection**: 100% of step-9 stale writes (and, on the
  satellite leg, every stale-after-reclaim pin) return the fence rejection
  (`ErrClaimRejected`), never success and never a generic error; a version
  mismatch stays distinct from a rejection.
- **Reclaim**: every expired lease is reclaimed within
  `lease_ttl + reap_interval + one reap pass`; record p50, p99, and max.
- **Replay idempotence**: each replayed terminal call returns the same ack
  and changes no row beyond the first application (per section 3 for the
  satellite leg).
- **Starvation bound**: no eligible item waits longer than
  `starvation_bound_seconds`; no scope goes unclaimed while below budget.
- **No overshoot**: grants per budget bucket never exceed `quota_budget`
  on that leg's own granularity.
- **Zero deadlocks**: no Postgres `deadlock_detected` (SQLSTATE 40P01), and
  no lock wait above `lock_wait_bound_ms`; capture `pg_stat_activity` wait
  events at the sampling interval.
- **Zero loss**: created count = terminal count + live count at every sample;
  live = 0 at the end.
- **Drain to zero**: stop issuing work, let reap and replay finish, confirm
  zero nonterminal synthetic items and zero live synthetic claims, and queue
  depth back to the pre-run baseline.
- **Residue**: rows still carrying `synth-8875-rehearsal-` in the
  satellite's `workflow_runs`, `workflow_work_items`, `workflow_claims`,
  `workflow_claim_pacing`, and quota state after cleanup = 0 (first zero);
  change in counters and rows without that prefix versus the pre-run snapshot
  = 0 (second zero, which includes `parity-%` rows belonging to the rehearsal
  lane); and core QA-environment database rows carrying the prefix = 0 (third zero, a
  read-only count, since no core write is granted).

## 7. Abort triggers (stop at once, run the reversal)

Aligned with the grant: abort on a dual claim, a fencing-token regression, a
lost item, or a non-synthetic id touched. Also abort on:

- a residue query returning non-zero at any checkpoint;
- a `deadlock_detected`, or a lock wait above `lock_wait_bound_ms`;
- the satellite database crossing any limit in `pg_headroom`;
- the deployed target leaving its recorded authority state (a real claim
  appears on a real item, or the mode changes from the recorded value);
- `wall_clock_cap` expiring;
- the grant expiring or the revert PR merging.

## 8. Reversal

1. Stop all synthetic workers; stop replays.
2. Drain: let reap reclaim and workers finish until live synthetic claims = 0.
3. Delete synthetic rows by the prefix `synth-8875-rehearsal-` only, child
   tables first (claims, pacing, quota state, then work items, then runs), in
   bounded batches; rerun the residue queries until all three zeros hold. On
   the in-tree leg the harness force-drops its disposable database at test end.
   Never `TRUNCATE`.
4. Merge the pre-authored revert PR (`iac_revert_pr`) to return the target to
   dark (`--deployment-mode=dark`, no `--claims-enabled`) and confirm the
   in-tree coordinator is still the sole authority.
5. Record the post-run snapshot next to the pre-run snapshot.

Reversal needs no downgrade migration: the run adds no schema, so the
standing rollback policy (`PLANE_DECLARATION_9376.md` §7) holds.

## 9. Evidence to capture

Per leg and seed: command lines, seed, digest, `intree_database_name`,
start/end timestamps, ops count per operation, rejection counts by type,
reclaim latency percentiles, max lock wait, deadlock count, sampler output,
the residue queries and their results, and the `NOT_APPLICABLE` records from
section 3. Paste exact numbers and a human duration; label totals that are
not comparable across legs.

## 10. What passing does and does not prove

Passing proves the deployed claim-plane holds the section 6 criteria at the
recorded concurrency and backlog, bound to one digest, and only for the steps
each leg runs. It does not prove core's generation-level fencing
(ABSENT-on-main), does not stand in for the throughput probe
(`REMOVAL_PREP.md` §4), and moves no authority. A new digest invalidates it;
rerun.
