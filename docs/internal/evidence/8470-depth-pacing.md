# #8470 Depth-Pacing Evidence

Claim issuance paces grants on per-domain pending depth first, with global
pending depth as the ceiling, under one shared hysteresis latch (#8470).
The pure verdict is `workflow.DecideClaimPacing`; the store consult is
`WorkflowControlStore.CheckClaimPacing`; paced grants return
`workflow.ClaimPacingDeferral` on the existing retry-after path while the
dispatcher skips paced targets work-conservingly and the runner backs off
`max(poll, grant delay)`. This note records the measured numbers, the four
ADR design-question dispositions, and the threshold-derivation method.

## Design-question dispositions

The pacing ADR leaves four questions for the owner; #8470 settles each
from source evidence, recorded here for owner review:

1. **Quota schema location** — out of scope. #8470 carries no quota code;
   quotas are the second mechanism with their own leaf.
2. **Pacing scope (first- vs third-party)** — uniform at claim issuance.
   The claim path cannot distinguish them by kind without an
   operator-maintained exemption list, and no acceptance criterion asks for
   one. The per-domain signal already limits blast radius to the saturated
   family.
3. **Threshold authority** — static config (`ClaimPacingConfig`, validated,
   zero value disables). Starting values below are harness-derived per the
   ADR prescription; the operator owns them.
4. **Domain granularity** — per-domain is the collector family
   `(collector_kind, collector_instance_id)`: `conflict_domain` exists only
   on `fact_work_items`, which the claim path never reads, while the claim
   is partitioned by family. The #8468 fairness-key depth observer keeps
   the key-level breakdown for operators.

No arbiter escalation: nothing above was genuinely unsettled once the claim
path's readable columns and the acceptance criteria were laid out.

## Performance Evidence:

Backend: PostgreSQL 18.6 (Debian pgdg, throwaway container `pg-8470-proof`)
on aarch64; Go live-test suites on the same host via `ESHU_POSTGRES_DSN`.

- Theory shim first (throwaway, 50,000 pending rows): the grouped
  per-family depth scan plans a bitmap heap scan over all 50,000 pending
  rows at 8.3 ms with 1049 buffers, against a ~0.18 ms index-served claim.
  A synchronous per-claim depth read would cost ~46x the claim at that
  backlog, so production consults reuse one depth observation for
  `DepthCacheTTL` while the latch row stays freshly read. No new index: the
  existing family-queue-depth shape serves the scan.
- Pacing-idle drain (`TestWorkflowClaimPacingIdleDrainHoldsFloor`): eight
  claimers drain the 580-item skewed backlog exactly once at 434.4/s
  against the 119 design floor, zero paced waits, eight distinct backends,
  minimum ten grants per claimer. The same backlog shape drains at 478.7/s
  unpaced, so the per-attempt consult costs ~9% with the cache disabled
  (TTL=0, the deterministic test shape); the production TTL amortizes it.
- Paced drain (`TestWorkflowClaimPacingEngagedDrainCompletesExactlyOnce`):
  40 items with every consult pacing (20 ms grant delay, 76 paced waits)
  still drain exactly once at 111.1/s with all eight claimers sharing
  work. Pacing delays grants; it does not serialize or strand them.
- Runner backoff (`TestClaimedServiceBacksOffGrantDelayOnPacingDeferral`):
  one paced attempt then a grant completes in 0.10 s against a 5 ms poll,
  proving the delay honored is the 100 ms grant delay, and the paced
  attempt issues no claim.

Threshold derivation (method, for the operator): thresholds are
`drain_rate × tolerable_intake_pause`. At the measured ~430-480 claims/s on
the reference host, a family backlog of ~2,500 pending items is ~5 s of
intake, and a global backlog of ~25,000 is ~1 min. Starting points:
`DomainHigh` in the low thousands with `DomainLow` at half, `GlobalHigh`
an order of magnitude above with `GlobalLow` at half, `GrantDelay` of
seconds, `MaxEngagement` of minutes. The mechanism ships disabled (zero
config) until the operator wires these.

## Observability Evidence:

- `eshu_dp_workflow_claim_pacing_engaged_total` (by `pacing_signal`)
  counts latch engagements at the store consult chokepoint; transitions
  only, so sampling between engagements cannot read as "fine".
- `eshu_dp_workflow_claim_pacing_active` (up/down by `pacing_signal`)
  answers "is pacing on right now" (+1 engage, -1 release).
- `eshu_dp_workflow_claim_pacing_stuck_total` (by `pacing_signal`) fires on
  every overdue verdict so a wedged throttle stays loud.
- `eshu_dp_workflow_claim_pacing_delay_seconds` records every paced grant's
  delay, by `pacing_signal` and `domain` for the domain signal.
- Labels stay closed: `pacing_signal` is the `domain`/`global` pair, and
  `domain` is the operator-configured family set. Raw depths, thresholds,
  and timestamps stay off labels.

Coverage: `scripts/verify-telemetry-coverage.sh` green, operator dashboard
`Claim Pacing (engaged, active, stuck)` panel (9/9 generator checks),
registry, contract, and cardinality tests green.

## No-Regression Evidence:

`go test ./internal/workflow/ ./internal/collector/ ./internal/telemetry/
./internal/storage/postgres/migrations/` green; DDL/migration parity,
schema-order, checksum-manifest, and embed-digest gates green; the
pre-existing claim-depth and fair-claim live suites unmodified and green
(the drain loop gains an additive nil-default pacing hook only).
Pacing touches no worker count, batch size, lease duration, or
conflict-key partitioning: the serialization-workaround guard is the
pacing-idle floor above, not a claim about it.
