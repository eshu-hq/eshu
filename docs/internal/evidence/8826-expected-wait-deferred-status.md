# #8826 expected-wait status: regression and observability note

`security_alert_reconciliation` returns a retryable error while a package has
owned dependency evidence but no impact finding yet. That wait is designed: the
queue retries it for the smaller of `ESHU_REDUCER_MAX_ATTEMPTS` (default 3) and
the handler's own cap of 3 attempts
(`pendingImpactSecurityAlertReconciliationMaxAttempts`: at attempt 3 the handler
writes instead of returning the wait error). A larger queue budget therefore
does not lengthen this wait. The reducer
logged every such attempt as ERROR `reducer execution failed` and counted it in
`eshu_dp_reducer_executions_total{status="failed"}`, so a healthy wait read as a
failure.

The `deferred` status from #7597 only covered non-counting readiness classes, so
the wait could not simply be enrolled: enrolment would also stop the queue
counting the attempt, which is the attempt-accounting change #8827 owns. Instead
an error can declare `ExpectedWait() bool` (`reducercontract.IsExpectedWait`).
`Service.recordReducerResult` records `deferred` and logs WARN
`reducer execution deferred` for such an error while
`intent.AttemptCount < Service.RetryMaxAttempts` (wired from the queue's
`MaxAttempts`; zero mirrors the queue default of 3). The final attempt, which the
queue dead-letters, and every real handler error stay `failed` and ERROR.

No-Regression Evidence (#8826): baseline `origin/main` 238b4015a9, after the
change on branch `drive/8826-deferred-status`. No SQL, Cypher, queue claim,
lease, retry delay, `MaxAttempts`, attempt counting, ordering, goroutine or
batch-size change. The only runtime delta is one extra type assertion and an
integer compare in the recorder after a failed execution, which is not a hot
path. `go test -race ./internal/reducer ./internal/reducer/contract
./internal/reducer/securityalert ./cmd/reducer` passes (2153 tests), including
the failing-first tests in `service_expected_wait_test.go` (attempts 1 and 2 of 3
record `deferred`/WARN; attempt 3, a plain error, and a retryable non-wait error
record `failed`/ERROR) and the handler assertions that the real pending-impact
error is an expected wait.

Observability Evidence (#8826): no instrument, label name or log key was added.
The existing `status` label on `eshu_dp_reducer_executions_total` and the log
`status` field gain no new value; `deferred` now also applies to
`domain="security_alert_reconciliation"`. A deferred attempt logs WARN
`reducer execution deferred` with `failure_class=
security_alert_reconciliation_waiting_for_impact`; the exhausted attempt logs
ERROR `reducer execution failed` with the same class. The operator reference
(`docs/public/reference/telemetry/metrics-reducer-storage.md`), the telemetry
coverage row, and the 3 AM runbook ("Deferred reconciliation") name the status.
Dashboards and alert rules keyed on `status="failed"`
(`docs/dashboards/reducer.json`, `docs/dashboards/pipeline-slo.json`) need no
change: they now exclude the expected wait and still count the exhausted attempt.
