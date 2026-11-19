# #10870 Stop the code-reachability check from scanning when nothing is ready

Refs #10870. Part of #10860 (phase 1, idle checks, step 2).

## What changed

One PR, no DDL, no candidate-SQL change, no new index:

- The runner is idle-gated (`code_reachability_projection_idle.go`): each
  cycle first runs the cursor probe (`CodeReachabilityStore.
  LatestCompletedIntentTime` — one per-domain newest `completed_at`, two
  backward LIMIT-1 index-only scans on the existing
  `shared_projection_intents_pending_idx`), and runs the full candidate
  query only when the probe advanced past the cursor, the probe failed, the
  probe regressed (reopen/failover), the cursor is unknown (startup), or
  the audit interval elapsed.
- Empty cycles back off exponentially from `PollInterval`, doubling per
  consecutive empty cycle, capped at `MaxPollInterval` (default 30s, env
  `ESHU_CODE_REACHABILITY_PROJECTION_MAX_POLL_INTERVAL`). A productive
  cycle resets to base and re-polls immediately.
- The full candidate query doubles as the periodic audit at `AuditInterval`
  (default 15m, env `ESHU_CODE_REACHABILITY_PROJECTION_AUDIT_INTERVAL`),
  bounding every readiness transition that does not advance the cursor.
- Kill-switch `ESHU_CODE_REACHABILITY_PROJECTION_IDLE_PROBE=false`
  restores full-query-every-poll.

What this is not: no written short list (missed-writer staleness across
four-plus write sites), no backoff-only fix (the idle full query still
burns ~410k buffers per call), no LATERAL LIMIT-1 candidate rewrite
(measured 978ms / 246k buffers on the read copy — it keeps the 269k-buffer
ready gate), no index add/drop (`acceptance_lookup_idx` is the hottest
shared index at 4.98B scans; `acceptance_completed_idx` has 129M scans by
an unidentified consumer — both out of scope).

## Performance Evidence: before/after on the touched path

Before figures (pre-change, re-measured 2026-11-19 for this issue): the
QA PRIMARY `pg_stat_statements` row for the candidate statement (queryid
-2634298717410040388, stats_reset 2026-10-16): 1,737,618 calls, 13,090
rows, mean 1,129.5 ms, 410,960 buffers/call, 1,962,582 s total. The issue
text pins the older window: 1,674,092 calls in 31.8 days, 12,737 rows,
mean 1,116.7 ms, 410,586 buffers/call, 8.0% of all database query time.
The loop waits a fixed 500 ms (`sharedCfg.PollInterval`) and never backs
off — about 2 calls/s, finding something once in ~130 tries.

Theory proof (cheapest shim first, before the change): throwaway
`EXPLAIN (ANALYZE, BUFFERS)` on a disposable PostgreSQL 18 with a 500-run
idle fixture (every ready run already projected, candidate returns zero
rows yet pays the full scan), running the exact shipped statement text.
Candidate: 11,591 shared-hit buffers under the top Sort, execution
7.393 ms. Proposed probe on the same fixture: 5 execution buffers + 12
planning buffers, execution 0.036 ms — Index Only Scan Backward on
`shared_projection_intents_pending_idx`, heap fetches 0. Same-shape
confirmation at 20k completed intents: probe stays two backward tip scans,
8 execution buffers, 0.030 ms. The idle cost is O(1) in ready runs;
the candidate cost is O(ready runs x gate probes) plus O(completed
intents/run).

After figures: the candidate query text is unchanged, so its per-call cost
is expected unchanged — the claim is frequency plus idle cost, never a
per-call speedup. Post-deploy method (pinned before the change):
re-snapshot QA PRIMARY `pg_stat_statements` for the candidate statement
(re-derived by query LIKE `%code_reachability_repository_watermarks%` +
LIMIT at snapshot time) plus the new probe statement immediately
pre-deploy, delta over >= 24h post-deploy (1h minimum only with rate
math), reporting calls/sec, mean ms, and buffers/call for the candidate
AND the probe separately. Pre-registered thresholds: candidate calls/sec
down >= 95% vs the ~0.6/s baseline (idle ceiling = audits at 4/h plus
change-triggered fulls), probe mean <= 5 ms and <= 100 buffers/call on
primary, probe `EXPLAIN (ANALYZE, BUFFERS)` on a read copy <= 50 buffers.

Generic-plan note (does not affect the ruling — no SQL change ships):
`PREPARE` + 6x `EXPLAIN EXECUTE` on the disposable fixture shows the
generic plan uses nested-loop/seq scans at tiny scale while the custom plan
uses the acceptance/intent indexes; production runs the custom shape per
the QA `pg_stat_statements` buffer profile above.

## Observability Evidence: the changed path stays diagnosable

New signals (closed label sets, no scope/generation/repository values):

- `eshu_dp_code_reachability_projection_cycles_total` by kind
  (`probe_skip`, `probe_triggered_full`, `audit_full`, `productive`) —
  the idle-cost signal: only non-skip kinds run the full query.
- `eshu_dp_code_reachability_projection_inputs_found_total` by trigger
  (`probe`, `audit`) — an `audit` rate above zero is the short-list-miss
  class with its found count attached.
- `eshu_dp_code_reachability_projection_backoff_seconds` gauge — the
  current idle wait, 0 on productive re-poll.
- Every audit-forced full query logs `code reachability projection audit
  completed` with `input_count` including zero, so the audit cadence is
  visible even when it finds nothing. A productive probe-triggered cycle
  keeps the existing `code reachability projection completed` log.

Coverage contract row added for `code_reachability_projection_idle.go`
in `docs/public/observability/telemetry-coverage-reducer-core.md`
("projection (code reachability idle probe)").

## Proof the audit catches a miss, and the newly-ready bound

Ack atomicity (arbiter-unresolved, settled here): `ReducerQueue.Ack`
(`go/internal/storage/postgres/reducer_queue.go`) marks a claimed work
item `succeeded` in a standalone `UPDATE`, after the domain handler's own
intent-emission transaction commits — and the downstream projection
runner completes those intents in between. So a work ack CAN land after
all intent completions with no new `completed_at`: the
ack-after-completion cursor-miss class is real, and the audit (not the
probe) covers it. The seeded-miss live test uses exactly this transition.

Live proof (`TestIdleProbeLiveProof`, disposable PostgreSQL 18,
`ESHU_REACHABILITY_IDLE_PROBE_PROOF_DSN`): probe returns the newest
completed intent across both domains with the pinned plan (Index Only
Scan Backward on `shared_projection_intents_pending_idx`, execution
buffers <= 30 at 20k-intent fixture scale); a run whose readiness flips
only via the second materialization ack is skipped while probe-equal
(no watermark) and projected by the audit (watermark stamped at the
intent `completed_at`); a freshly completed pending intent projects on
the next cycle without waiting for the audit; two concurrent runners over
one ready run converge on a single identical watermark.

Pickup bounds (stated in `go/internal/reducer/codeintel/README.md`):
(a) intent-completion-driven readiness: at most `MaxPollInterval` (30s)
after the completing intent commits, plus one full-query/batch drain —
proven by the fake-clock `Run` wait-sequence tests (base, 2x, cap;
productive reset to base with immediate re-poll), no wall-clock sleeps.
(b) every other transition: at most `AuditInterval` (15m) plus the same
drain — proven by the audit-due runner test and the live ack-flip
section. Competing writers are unchanged: the candidate SQL and its
claim-free idempotent DELETE+INSERT partition writes are untouched, and
the probe is a read with no claim semantics; the live two-runner section
reuses the partition-concurrency pattern asserting identical rows and
watermark.

## Verification

Focused gates on the final tree (exact commands, exit codes observed):

- `go test ./internal/reducer/codeintel/ -count=1` — PASS (55 tests).
- `go test ./internal/storage/postgres/code/reachability/ -count=1` —
  PASS (live tests skip without a DSN).
- `ESHU_REACHABILITY_IDLE_PROBE_PROOF_DSN=... ESHU_REACHABILITY_IDLE_PROBE_PROOF_DISPOSABLE=1 go test ./internal/storage/postgres/code/reachability/ -run '^TestIdleProbeLiveProof$' -count=1` — PASS.
- `go test ./internal/telemetry/ -count=1` — PASS.
- `go test ./internal/reducer/ -run 'TestServiceRunStartsCodeReachabilityProjectionRunner' -count=1` — PASS.
- `go vet` on the four touched Go trees — PASS, no findings.
- `scripts/verify-performance-evidence.sh` markers (`Performance
  Evidence:`, `Observability Evidence:`) present in this file.

Deferred to the coordinator per the leaf contract: `make pre-push`,
`make pre-pr`, `ci-gates review-attest`, push, PR creation, and the
post-deploy `pg_stat_statements` delta against the pre-registered
thresholds above.
