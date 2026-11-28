# Holder-watermark reopen cost (#11613 PR1)

Refs #11613. The holder reopen gains one primary-key probe of
`supply_chain_impact_scope_pass_state` (the holder watermark), the shadow
freshness scalar on the outer select, and the succeeded-arm stamp union.
This note prices that added work and names the would-skip signal.

No-Regression Evidence (#11613): holder reopen, PostgreSQL 18.6, seed of 2,000 holder scopes with one work item each across mixed statuses plus 1,000 watermarked pass-state rows, vacuumed and analyzed. Base statement median 0.061 ms, mean 0.067 ms; head statement median 0.077 ms, mean 0.084 ms; the added cost is 0.016 ms per reopen, about a tenth of a second across a 6,953-holder wave. Plan shape, buffer profile, lock footprint, and ordering are otherwise unchanged: the same index probes on the scope-generation and scope primary-key indexes, no sequential scans, one added primary-key index scan with three buffer hits, the single `FOR NO KEY UPDATE` row lock still on the work row only, and the same work-item ordering. The relative rise on a sub-tenth-millisecond statement is reported as measured; the absolute cost is the floor for reading one durable row, and no skip is enabled, so reopened work is unchanged.
Observability Evidence (#11613): a succeeded reopen that meets every freshness condition counts one point on `eshu_dp_supply_chain_impact_fanout_total` with outcome `shadow_would_skip`, apart from `pending_noop`, with the same count on the `shadow_would_skip` field of the fan-out log line, the `shadow_would_skip` attribute of the `supply_chain_impact.fanout` span, and the `fanout_shadow_would_skip` result sub-signal. The row still reopens; the outcome only measures the would-skip share PR2 decides on.
