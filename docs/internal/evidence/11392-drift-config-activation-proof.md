# #11392 drift config-activation settle proof

Refs #11392. The #7635 producer-activation settle gains one config-leg
probe plus one config-leg listing so a config-only activation (a repo
snapshot carrying `terraform_backends` parser facts, moving the anchor the
drift reader resolves) reopens drift instead of retiring inapplicable.
Every figure below is a hermetic fake-drive figure: SQL text never
executes, so there is no live plan to profile and no QA gain claimed.

## No-Regression Evidence: per-activation settle budget 6 -> 8, all anchored

Baseline: pre-fix `SettleClaimedProducerActivation` on the pre-fix
9-response staging issues 6 `fact_records` statements (3 probes: evidence,
OCI keys, drift ARNs; 3 listings: two OCI families, drift), outcome
`completed`, `counts=map[aws_cloud_runtime_drift:1]`. Measured 2026-11-24
on `ffa5002136` in a throwaway worktree with a scratch drive (same
counting rule as the budget test; scratch file removed afterwards):
`go test ./internal/storage/postgres/ -run
TestScratchBaselineSettleFactProbeCount -v -count=1` ->
`baseline fact_records statements=6`, `--- PASS` (`ok ... 0.049s`).

After: one completed settle issues exactly 8 `fact_records` statements (4
probes: evidence, OCI keys, drift ARNs, config kinds; 4 listings: two OCI
families, drift, config), pinned by
`TestProducerSettleFactProbeBudgetHermetic`. Measured 2026-11-24 at this
head: `go test ./internal/storage/postgres/ -run
'TestProducerSettleReopensDriftOnConfigActivationHermetic|TestProducerSettleFactProbeBudgetHermetic|TestProducerConfigActivationWithoutIntersectionReopensNothingHermetic|TestProducerBackendlessConfigActivationRetiresInapplicableHermetic'
-v -count=1` -> all four `--- PASS` (`ok ... 0.120s`).

Backend/version: N/A (hermetic). Input shape: one claimed obligation
(`scope_id=git:iac-demo`, `generation_id=config-gen-2`) with owned keys
`[s3]` and one converged drift item,
`drift-item-aws-ec2/aws_cloud_runtime_drift`. Terminal row counts: the
config reopen settles `completed` with exactly one
`aws_cloud_runtime_drift` row reopened; the disjoint-kind drive (`gcs`
vs s3-owned states) completes with zero reopens; the backend-less config
generation retires `inapplicable` with zero reopens; the second settle
reopens zero (exactly-once).

Why safe (no-storm): the delta is +2 statements per activation, each
`(scope_id, generation_id[, fact_kind])`-anchored, never a corpus scan.
Reopen is exactly-listed (only IDs the linkage listings return), bounded
by the `updated_at < owed-at` staleness conjunct, and terminal
(completed/inapplicable/obsolete) so a retry cannot double-count. Bulk
import stays `O(keys + dependents)`: the kinds probe reads one
generation, the listing probes the floored candidates' scopes only. The
kind-level (not exact-locator) ownership hop costs at most one wasted
idempotent wake-up; the ARN intersection is the exact bound and the
reducer re-run is the authority.

## No-Observability-Change: existing producer-activation instruments cover the config leg

The change adds no spans, metrics, or log lines (`rg -i
'otel|metric|span|tracer|slog'` over the Go diff matches nothing). The
config leg settles through the same consumer path, so an operator watches
the existing instruments in
`go/internal/telemetry/instruments_producer_activation.go`:
`eshu_dp_producer_activation_settle_total` by outcome (a config-leg storm
would show as `completed` settle growth),
`eshu_dp_producer_activation_reopened_total` by consumer domain (drift
reopens land under `aws_cloud_runtime_drift`),
`eshu_dp_producer_activation_failures_total` by reason, plus the
`eshu_dp_producer_activations` status gauge and the oldest-open-age
gauge; the consumer's per-obligation log line carries the scope and
generation id.
