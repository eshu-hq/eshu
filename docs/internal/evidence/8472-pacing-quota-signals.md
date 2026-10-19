# #8472 Pacing And Quota Signals Evidence

#8472 makes the claim-pacing throttle legible from metrics alone: the
per-domain depth observer that feeds the pacing verdict is now itself an
operator signal, next to the engagement, state, and delay series #8470
shipped. This note records what was added, the label disposition against
the design text, and the recorded deferral for the quota fourth metric.

## What #8472 adds

- `eshu_dp_workflow_claim_pacing_depth_pending` — synchronous `Int64Gauge`
  recorded in `WorkflowControlStore.CheckClaimPacing` from the same depth
  observation that feeds `workflow.DecideClaimPacing`: the claiming
  family's pending depth with the `domain` label (`kind/instance`, the
  operator-configured family set, so the label stays bounded) and the
  system-wide pending depth without it, following the pacing-series
  convention that the global ceiling carries no `domain` label.
- No new query, index, or storage read: the gauge reuses the consult's own
  grouped pending-depth observation (one scan per TTL window, unchanged).
  The added cost per consult is two in-memory OTEL recordings.
- Coverage row extended in place (`collector.claimed_run`); operator
  dashboard `Claim Pacing (engaged, active, stuck, depth)` panel gains per-
  family and system-wide depth targets (9/9 generator checks).
- Regression proof:
  `TestWorkflowClaimPacingRecordsDepthPendingGauge` seeds 60 pending in
  one family, consults, and asserts the verdict paces while the gauge
  reads 60 on both the `domain` series and the unlabeled global series.

## Label disposition (design text vs shipped series)

The backpressure ADR sketches the engaged counter as labelled by signal
(`per_domain` / `global`) and conflict domain. The shipped series (#8470,
kept by #8472) use `pacing_signal` in the closed `domain` / `global` set
with `domain` naming the saturated collector family as
`collector_kind/collector_instance_id`. Renaming `domain` to `per_domain`
now would break the shipped series, the dashboard, and every proof that
asserts on them, for a purely lexical gain; the operator reference
(`docs/public/reference/telemetry/claim-pacing.md`) pins the mapping
instead. `conflict_domain` exists only on `fact_work_items`, which the
claim path never reads, so the family is the only actionable unit here.

## Quota integration: deferral dissolved on #8557 merge

The fourth metric, `quota_exceeded` (counter by component and quota kind),
is implemented by the merged #8557 path, not on this branch — and this
branch registers no duplicate. Verified against the rebased tree: the
mechanism lives in `go/internal/storage/postgres/workflow_control_quota.go`
(`recordQuotaThrottle`: one tick per claim that found eligible work but
granted nothing because the quota blocked it; `recordQuotaFallback` for
the unreadable-state floor path), the instruments
(`eshu_dp_workflow_claim_quota_exceeded_total` labelled `collector_kind` +
`quota_kind` with the closed `generations_per_hour` value,
`eshu_dp_workflow_claim_quota_unreadable_fallback_total` labelled
`collector_kind`) are registered in `go/internal/telemetry/instruments.go`
under contract `go/internal/telemetry/contract/workflow_claim_quota.go`,
the operator reference is `docs/public/reference/telemetry/claim-quotas.md`,
and its live proofs are `workflow_control_quota_*_live_test.go`. The
labelling matches the design (counter by component + quota kind).

What this branch integrates instead of deferring: the `collector.claimed_run`
coverage row now names the pacing depth gauge next to both quota series
and cites `workflow_control_quota.go` as a stage owner, and the
claim-pacing reference boundary note points at the merged quota reference.
Counter-level quota proofs (asserting the tick) stay with the #8473 leaf,
which owns pacing failure-story proofs on signals.

## No-Regression Evidence:

Focused suites and verifier runs are recorded in the #8472 commit
message trailer.

## Observability Evidence:

New gauge registered, emitted at the pacing consult chokepoint, covered
by the extended `collector.claimed_run` row; verifier and dashboard
generator checks green (see the #8472 commit message trailer). No new
label key (`domain` reused); no high-cardinality value (family set only).
