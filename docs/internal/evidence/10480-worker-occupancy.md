# Worker occupancy telemetry

## No-Regression Evidence (#10480):

Baseline: batch-claim worker occupancy was invisible — stall triage
required pg_stat forensics plus log scraping, and no per-worker signal
existed. After: a mutex-guarded per-worker tracker (current intent plus
execution start) feeds a gauge, span attributes, and a start log line on
the existing telemetry family with no new labels. Backend: unit tests
with SDK readers plus focused reducer/telemetry suites (7 new tests
green, full suites green, worker subset green with race detector; no
touched test needs a live database). Input shape: table-driven unit
cases including nil tracker, out-of-range indexes, and
release-then-read. Terminal counts: all green, zero behavior change to
scheduling, locking, timeouts, or claim flow.

## Observability Evidence (#10480):

The gauge `eshu_dp_reducer_batch_worker_execution_age_seconds`
(closed `queue`, `worker`, `domain`) plus the start log line give an
operator the stuck worker, domain, and exact intent from metrics or
logs alone; the telemetry coverage verifier agrees and the operator
runbook points at both surfaces.
