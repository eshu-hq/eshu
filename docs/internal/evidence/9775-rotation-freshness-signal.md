# Owned-package rotation freshness signal (#9775)

No-Regression Evidence: scheduling is unchanged. The slot count, the rescan interval and the rotation walk are untouched, so baseline and after match: 1,024 slots at 30 minutes is a 512 h rotation (QA target count 32,726, from the issue). The new path stores one small struct per instance under a mutex per activation; the gauges are read by a metrics callback. No query is added. Per-activation cost was not timed (NOT_CHECKED); input shape and row counts do not apply.

Observability Evidence: `eshu_dp_owned_target_rotation_period_seconds`, `eshu_dp_owned_target_max_staleness_seconds` and `eshu_dp_owned_target_rotation_exceeds_max_staleness`, driven through the real service by `TestActivationReportsOwnedTargetRotationFreshness`. Live QA gauge values are NOT_CHECKED.

Why it is safe: the `max_staleness` key is optional, old decoders ignore it, and a blank value takes the 24 h default.
