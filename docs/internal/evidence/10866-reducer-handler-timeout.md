# #10866: Enforce a time limit on reducer work — implementation evidence

Issue: `eshu-hq/eshu#10866` (part of #10860, follow-up to #8211).
Branch: `drive/10866`. Status: mechanism + docs landed with the shipped
default 0 (unbounded). Do NOT close #10866 on this change: criterion 1
(measured p99 per kind) and criterion 5 (hosted alert rule) are NOT_CHECKED
below and need in-drive follow-up.

## What changed

- `go/internal/reducer/service_handler_timeout.go` (new): counting retryable
  `reducer_handler_timeout` class (`HandlerTimeoutFailureClass`,
  `handlerTimeoutError`), `ownDeadlineExpired` relabel guard, shared
  `releaseStuckHandler` tail, `failTimedOutHandler`/`failTimedOutBatchHandler`,
  `recordReducerHandlerTimedOut`.
- `go/internal/reducer/service_heartbeat.go`: `executeHandler` enforces the
  bound in two layers (derived execution-context timeout + deadline branch;
  buffered outcome > shutdown > bound; downstream `DeadlineExceeded` never
  relabeled); `failAbandonedHandler`/`failAbandonedBatchHandler` delegate to
  the shared tail (behavior identical, counter differs).
- `go/internal/reducer/service.go`, `service_batch.go`: timeout errors route
  to the timeout tails; cooperative timeouts additionally fire the timeout
  counter on the normal error path.
- `go/internal/telemetry/instruments.go`: `eshu_dp_reducer_handler_timed_out_total`
  counter; `reducerRunBuckets` extended with 1800/3600 (QA saw 1,510s and
  36,553s runs against the old 900s ceiling); `ReducerHandlerDuration` comment
  updated.
- `go/internal/telemetry/instruments_reducer_handler_timeout.go` (new):
  `eshu_dp_reducer_handler_timeout_seconds` gauge (per-domain bound, 0 =
  unbounded) + `RegisterReducerHandlerTimeoutGauge`.
- `go/cmd/reducer/observed_service_wiring.go`: gauge registered from the
  built `Service` over all `KnownDomains`.
- `go/cmd/reducer/handler_timeout_config.go`: explicit `0s` disables the
  bound; negative/garbage still fail startup. `main.go`: startup log is
  `reducer handler timeouts enforced/disabled` (replaces the #8211
  recorded-but-not-enforced warning).
- Docs: `go/cmd/reducer/README.md` timeout rows,
  `go/internal/reducer/queue-and-runners.md` release contract,
  `go/internal/reducer/telemetry.md`,
  `docs/public/reference/telemetry/metrics.md` (alert PromQL),
  `docs/public/reference/telemetry/metrics-reducer-storage.md`.

## Criterion status

1. p99 per kind then limits — NOT_CHECKED. QA metrics were unreachable
   in-drive, so no numeric default is shipped (default stays 0) and the
   histogram buckets were extended (900 -> +1800/+3600) so the next
   measurement can resolve above the old ceiling. When QA is reachable:
   `histogram_quantile(0.99, sum by (domain,le)
   (rate(eshu_dp_reducer_handler_duration_seconds_bucket[7d])))`,
   then set per-domain overrides >= 2x measured p99 and >= longest
   legitimate observed run.
2. Enforce via `Service.HandlerTimeoutFor` — DONE. Single shared
   enforcement point (`executeHandler` + the two caller tails); zero
   production callers existed before, both callers route now.
3. Late-write safety — GATED, audited for the two incident domains (table
   below); all other domains get NO shipped bound until audited. Proof
   tests: `TestSupplyChainImpactWriterRejectsSupersededPassBeforeUpsert`
   (stale fencing token rejected, pre-existing),
   `TestBatchInsertFactsRepeatedStableFactIDUpserts` (repeat stable-fact
   write upserts, new), `service_fail_claim_rejected_test.go` + batch stale
   ack tests (late queue Ack/Fail fenced, #7142, pre-existing),
   `TestReducerHandlerTimeout...` integration tests (heartbeat stopped
   before Fail, new).
4. Repeat-timeout fate — DONE. Counting class: retries under
   `ESHU_REDUCER_MAX_ATTEMPTS`, dead-letters with the class preserved when
   the budget is spent. Proof: `TestTimeoutFailureClassIsCountingRetryable`,
   `TestReducerQueueFailRetriesTimeoutClassWithinAttemptBudget`,
   `TestReducerQueueFailDeadLettersTimeoutClassWhenBudgetExhausted`.
   Non-counting and immediate-dead-letter were rejected (infinite poison
   loop / accuracy loss); `graph_write_timeout` reuse was rejected (would
   falsely trigger #3560 backpressure).
5. Execution-age alert — MECHANISM DONE, HOSTED RULE NOT_CHECKED. In-repo
   gauge + PromQL are wired/documented; no alert YAML exists in this repo
   (alerts live outside it), so the hosted rule needs a follow-up in the
   goal issue ledger: `eshu_dp_reducer_batch_worker_execution_age_seconds >
   on(domain) eshu_dp_reducer_handler_timeout_seconds` plus
   `increase(eshu_dp_reducer_handler_timed_out_total[10m]) > 0`.
6. Docs — DONE (files listed above).

## Per-domain late-write audit (criterion 3 gate)

| Domain | Write path | Verdict |
| --- | --- | --- |
| `security_alert_reconciliation` | Stable `FactID`/`StableFactKey`
  (`securityalert/reconciliation_writer.go:88-104`); batch insert is
  `ON CONFLICT (fact_id) DO UPDATE` (`factwrite/batch_insert.go`); paged
  evidence loop threads `ctx` to every page query
  (`facts_active_security_alert_reconciliation.go:62-91`) | SAFE for an
  operator-configured bound (idempotent upsert + cooperative cancel) |
| `supply_chain_impact` | Monotonic `SupplyChainImpactFencingTokenIssuer` +
  `pg_advisory_xact_lock` admission gate (`writer_admission.go`,
  `writer_retract.go:124-146`, migration 154); evidence loaders thread
  `ctx` (`supplychain/core/evidence_load.go`) | SAFE for an
  operator-configured bound (stale pass rejected in-write) |
| All other 65 `KnownDomains()` | Not opened in this change | NOT AUDITED:
  no shipped bound; operator-configured bounds carry the documented
  residual-risk warning |

Reproduce the domain set: `KnownDomains()` in
`go/internal/reducer/contract/domain.go` (67 entries at time of writing).

## Verification (this worktree)

- `go test ./internal/reducer/ -run 'Timeout|TimesOut|TimedOut|OwnDeadlineExpired|Abandon'`
- `go test ./internal/storage/postgres/ -run TimeoutClass`
- `go test ./internal/telemetry/ -run HandlerTimeoutGauge`
- `go test ./cmd/reducer/ -run HandlerTimeout`
- Full touched-package suites + `go vet` + selected gates before push
  (coordinator owns `make pre-push`/`make pre-pr`).
- `TestSchemaBootstrapCoordinationDefaultsFitTheJobDeadline` fails in this
  environment (needs `ESHU_DEPLOY_CHECKOUT` pointing at an eshu-deploy
  checkout); pre-existing, file untouched by this change.

No-Regression Evidence: the bound-0 (default) execution path adds one
`HandlerTimeoutFor` map lookup and one branch per handler run — no timer,
no extra goroutine, no lock. Baseline (origin/main `b9267c2336`,
pristine worktree) vs after (this branch, code identical to the measured
tree — only this evidence file changed afterward), same command,
same host, `go1.26.11 linux/arm64`:
`go test ./internal/reducer/ -run Abandon -count=1` -> 5.021s baseline vs
5.023s after (both dominated by the pre-existing 5s backoff test; delta is
noise). Full `go/internal/reducer` suite green after (5.5s), `-race`
green on the timeout/abandon subset, `go vet` clean on all touched
packages. No benchmark gate: one timer per claimed intent is the only new
allocation, and only when a bound is configured.

Observability Evidence: `TestReducerHandlerAbandonmentMetricsEmitted`
(pre-existing) plus `TestReducerHandlerTimedOutMetricsEmitted`,
`TestRegisterReducerHandlerTimeoutGauge_ExportsBoundsPerDomain`, and the
per-path counter/duration assertions in the timeout integration tests all
green; `telemetry` and `cmd/reducer` suites green.
