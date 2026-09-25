# Seconds Histogram Buckets

Issue #7084: seven `_seconds` histograms were registered without explicit
bucket boundaries and fell back to the OTel default set (0, 5, 10, 25, ...,
10000), which is sized for milliseconds. For operations measured in seconds,
every sub-second sample landed in `le="5"`. On ops-qa (reducer `sha-763c65e`,
2026-09-24), all 2,629 samples of `eshu_dp_queue_claim_duration_seconds` sat in
that bucket (sum 1052.8 s, mean 0.40 s), so any `histogram_quantile` over the
series returned an interpolated value between 0 and 5 unrelated to the real
claim tail.

No-Regression Evidence: this changes only the bucket boundaries declared at
instrument registration (`WithExplicitBucketBoundaries`) and adds
`WithUnit("s")` where it was missing. No metric name, label, recording call
site, hot-path code, query, worker, lease, or batch size changes. Recording
cost is unchanged in kind: the OTel SDK does one bucket search per observation
over 11 to 14 boundaries instead of the default 15. The new AST guard
`TestSecondsHistogramsHaveExplicitBuckets` runs only in tests. On the base, the
guard fails on exactly the seven offenders
(`eshu_dp_queue_claim_duration_seconds`,
`eshu_dp_shared_acceptance_upsert_duration_seconds`,
`eshu_dp_documentation_drift_generation_duration_seconds`,
`eshu_dp_identity_cache_reload_duration_seconds`,
`eshu_dp_identity_cache_probe_duration_seconds`,
`eshu_dp_gcp_cloud_freshness_lag_seconds`,
`eshu_dp_azure_freshness_lag_seconds`); on the head it passes. The seeded
violation and shadowed-name tests prove that the guard fails closed.

Observability Evidence: after deploy, `histogram_quantile(0.95,
rate(eshu_dp_queue_claim_duration_seconds_bucket{queue="reducer"}[5m]))`
resolves claim latency between 1 ms and 30 s instead of pinning to the
0-5 s bucket. Series names are unchanged, so existing dashboards and alerts
keep their queries. During a rolling deploy, old and new pods briefly export
different `le` sets for the same series; that aggregation artifact clears
once the rollout completes.
