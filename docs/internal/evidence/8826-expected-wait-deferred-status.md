# #8826 expected-wait status: regression and observability note

`security_alert_reconciliation` returns a retryable error while a package has
owned dependency evidence but no impact finding yet. That wait is designed: the
handler returns the wait error on attempts 1 and 2. At attempt 3
(`pendingImpactSecurityAlertReconciliationMaxAttempts`) it stops waiting and
writes `unmatched` decisions, so that attempt succeeds. At the default
`ESHU_REDUCER_MAX_ATTEMPTS` of 3 nothing dead-letters, and a larger budget does
not lengthen the wait. The reducer logged every such attempt as ERROR
`reducer execution failed` and counted it in
`eshu_dp_reducer_executions_total{status="failed"}`, so a healthy wait read as a
failure.

The `deferred` status from #7597 only covered non-counting readiness classes, so
the wait could not simply be enrolled: enrolment would also stop the queue
counting the attempt, which is the attempt-accounting change #8827 owns. Instead
an error can declare `ExpectedWait() bool` (`reducercontract.IsExpectedWait`).
`Service.recordReducerResult` records `deferred` and logs WARN
`reducer execution deferred` for such an error while
`intent.AttemptCount < Service.RetryMaxAttempts` (wired from the queue's
`MaxAttempts`; zero mirrors the queue default of 3). An attempt at the budget,
which the queue dead-letters, and every real handler error stay `failed` and
ERROR. For this class that only happens when the budget is 1 or 2: with budget
3 or more the handler resolves at attempt 3 and the attempt succeeds.

No-Regression Evidence (#8826, doc correction): baseline `origin/main`
f7a8d131e8, after the change on branch `drive/8826-docs-correction`. This
follow-up has no runtime delta: it edits comments, docs and one test case (the
`AttemptCount = 4` case in `securityalert/reconciliation_test.go`), and
`go test -race ./internal/reducer/securityalert` passes with it. The rest of
this paragraph is the earlier runtime change, kept as history, which was first
measured against 238b4015a9. It added no SQL, Cypher, queue claim, lease,
retry delay, `MaxAttempts`, attempt counting, ordering, goroutine or
batch-size change. The only runtime delta is one extra type
assertion and an integer compare in the recorder after a failed execution, which
is not a hot path. `go test -race ./internal/reducer ./internal/reducer/contract
./internal/reducer/securityalert ./cmd/reducer` passes (2154 passed and
22 skipped test events by `go test -race -count=1 -json`, measured on the
branch before this note was edited), including the failing-first tests
in `service_expected_wait_test.go` (recorder with a budget of 3: attempts 1 and
2 record `deferred`/WARN; attempt 3, a plain error, and a retryable non-wait
error record `failed`/ERROR) and the handler assertions that the real
pending-impact error is an expected wait and that the handler stops waiting at
attempt 3, whatever the queue budget.

Observability Evidence (#8826, doc correction): this follow-up changes no
instrument, label name, log key or status value; it corrects docs and code
comments only. The rest of this paragraph is the earlier runtime change, kept as
history: no instrument, label name or log key was added. The existing `status`
label on `eshu_dp_reducer_executions_total` and the log `status` field gain no
new value; `deferred` now also applies to
`domain="security_alert_reconciliation"`. A deferred attempt logs WARN
`reducer execution deferred` with `failure_class=
security_alert_reconciliation_waiting_for_impact`; a dead-lettering attempt
(budget 1 or 2) logs ERROR `reducer execution failed` with the same class. The
operator reference
(`docs/public/reference/telemetry/metrics-reducer-storage.md`), the telemetry
coverage row, and the 3 AM runbook ("Deferred reconciliation") name the status.
Dashboards and alert rules keyed on `status="failed"`
(`docs/dashboards/reducer.json`, `docs/dashboards/pipeline-slo.json`) need no
change: they now exclude the expected wait and still count real errors and
dead-lettering attempts. At the default budget the wait ends in a successful
write at attempt 3, so it never reaches those alerts.
