# #8469 Fair-Claiming Evidence

Three-step fair claiming in one PR (owner decision in
`docs/internal/design/4588-collector-claim-pacing.md` section 10): claim
index fix, three-segment fairness key with priority/ecosystem columns, and
priority-then-rotation ORDER BY. Each step carries its order-equivalence or
behavior proof in its commit; this note records the measured numbers behind
the gate markers.

## Performance Evidence:

Backend: PostgreSQL 18.6 (Debian pgdg) on aarch64, throwaway container for
theory shims; Go live-test suites on the same host against a second
throwaway 18.6 container via `ESHU_POSTGRES_DSN`.

- Step 1 theory (throwaway shim, 50,100 pending rows, all eligible): the
  shipped candidate shape planned an index scan over all 50,100 eligible
  rows plus a top-N sort, 7.1-7.6 ms with 612 buffers; with the
  `(collector_kind, collector_instance_id, status,
  (COALESCE(visible_at, created_at)), created_at, work_item_id)` expression
  index the same SELECT plans a first-row index serve at 0.032-0.050 ms
  with 4 buffers and no Sort node. The visibility OR stays a residual
  filter; LIMIT 1 keeps the serve flat in backlog size.
- Step 3 theory (throwaway shim, skew: 3000-row burst key plus four 25-row
  small keys, one tier): FIFO served the burst 40/40; the static
  priority-then-key order served the burst 40/40 (the clause-6 finding
  reproduced); the salted uniform pick over per-key heads served
  8/6/10/11/5 across the five keys. The production loose-scan shape
  (recursive distinct-pair walk, per-pair head probe, salted pick, LATERAL
  point-probe lock) plans fully index-served at 8.6-8.8 ms with ~300
  distinct keys (scales with distinct keys, flat in backlog; the DISTINCT
  ON alternative measured 51.7 ms and was rejected).
- Commit proof on real Postgres (`TestWorkflowClaimDepth*`,
  `TestWorkflowFairClaim*`, `TestWorkflowClaimCandidateUsesFairClaimIndex`):
  eight claimers drain 580 skewed items exactly once at 490.6 claims/s
  (design floor 119; even 320-item drain 328.4/s, unregressed against the
  315.5/s FIFO floor), every key first-granted at sequence <= 15 (small keys
  previously at >= 493, first-claim latency 25.26 s down to 4-21 ms),
  priority tiers drain strictly tier by tier, and a 40-claim fixed-salt
  lottery grants exactly the independently simulated draw sequence
  (9/6/9/10/6). No serialization: all eight backends pinned, minimum ten
  grants per claimer.
- Contention cost, measured openly: the barrier-synchronized back-to-back
  drain returns empty on ~2736 of ~3316 attempts (five draw slots, roughly
  five concurrent claim statements). Two disproven theories are recorded
  so nobody re-learns them: per-head FOR UPDATE inside a LATERAL locks
  every free head it probes rather than only the winner (removed in favor
  of a lock-free pick plus a single-row acquisition), and a fixed 1 ms
  retry sleep re-fires every collided claimer on the same millisecond
  (replaced with jittered 0-5 ms). Staggered starts change nothing, pinning
  the rate to the back-to-back duty cycle, not phase. Handler-separated
  production claims barely overlap; depth pacing (#8470) relieves the
  residual by spacing claims. A per-connection prepared claim statement
  would cut ~0.4 ms of planning per attempt and is left as follow-up work.
- Observer query cost (depth gauge refresh): 10.1 ms over 50,100 pending
  rows (sequential scan plus hash aggregate), same class as the existing
  family-depth gauge; it runs on the snapshot refresher, never on scrape.

## Observability Evidence:

- `eshu_dp_workflow_fair_claims_total` (by `collector_kind`,
  `claim_priority`) counts every granted claim at the collector dispatch
  chokepoint; the tier share proves the rotation serves every class.
- `eshu_dp_workflow_claim_wait_seconds` carries the `claim_priority` label
  for attributable starvation evidence per tier.
- `eshu_dp_workflow_fairness_key_depth` (top 10 `fairness_key` series per
  family, bound enforced in SQL) separates one enormous account from
  everything behind; `eshu_dp_workflow_fairness_key_count` carries the
  distinct-key denominator of the wait bound. Both are snapshot-served from
  one refresh query in `go/cmd/reducer`.
- The `collector.claimed_run` span carries the raw `fairness_key` and
  `claim_priority`; raw keys stay off every other metric label (closed 0-4
  rank everywhere else).

Coverage: `scripts/verify-telemetry-coverage.sh` green, operator dashboard
`Fair-Claim Rotation (per tier, per key)` panel (9/9 generator checks),
registry and contract tests green.
