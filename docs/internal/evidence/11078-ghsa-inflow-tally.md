# #11078 GHSA inflow tally: no-regression evidence

The winners dirty tally (migration 299) counted NVD-proxy and NVD scope rows
only, so GHSA — about 76% of dirty inflow — was entirely uncounted. This
note is the tracked record the perf-evidence gate requires for migration 325
and the sampler change that close the gap. Each tally row is one capture
minute of drained inflow: `nvd_proxy_rows` counts the CVE-% fact marks,
`ghsa_proxy_rows` the GHSA-% fact marks, and `nvd_scope_rows` the NVD scope
marks the maintainer deleted in that minute, each summed over
`dirty_version` so one log row counts every mark it collapsed.

## No-Regression Evidence:

This change adds one SUM FILTER and one summed column to the same drain
statement and one FILTER to the sampler's single log scan — same statements,
same locks, same single pass, no new index and no predicate on a hot read.
Baseline: the origin/main drain CTE (2 SUM FILTERs) and sampler (2 live
FILTERs plus min). After: 3 and 3 on the same statements. Backend:
PostgreSQL 18.6 (Ubuntu 18.6-0ubuntu0.26.04.1, aarch64) disposable bootstrap,
351 migrations applied. Input shape: 5 GHSA-% plus 2 CVE-% fact rows captured
inside the trailing hour, one maintainer drain tick. Terminal counts: the
tally holds 5 ghsa_proxy_rows and 2 nvd_proxy_rows; the sampler reads 5
GHSA, 2 CVE, 0 scope with the log fully drained (depth 0); all six
trip-sample live tests and the tally pin tests green, and the pre-existing
NVD rates unchanged. Why safe: additive output columns only; the drain still
admits exactly the union of the three predicates, and the live proof drains
and samples one database so any drift fails.

## Observability Evidence:

The hourly probe records
eshu_dp_nvd_baseline_ghsa_proxy_rows_per_hour beside the ruled gauge through
the extended RecordNVDBaselineDirtySample;
TestLoadNVDBaselinePlanInputsSamplesHourly asserts the 660 GHSA gauge value
from the fixture sample, and verify-telemetry-coverage.sh agrees.
