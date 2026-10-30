# Evidence: retryable failures carry retry_after_seconds (#9591)

## Change

`Runner.emitFailure` in `go/internal/collector/scannerworker/component/runner.go`
sets `RetryAfterSeconds: 60` on the failure status when the analyzer error
maps to the retryable state. Terminal failures still carry zero. The SDK
contract (`eshu-sdk/collector` validation) rejects a retryable failure
status with a zero delay, so previously every retryable analyzer failure
died as `invalid_result` terminal instead of retrying.

## Concurrency analysis

- Conflict domain: one `workflow_claims`/`workflow_work_items` row per
  work item, owned by the claiming extension pod via lease. The fix
  changes only the emitted page content, not claim acquisition, lease
  handling, or worker counts.
- Transaction scope: none (single-shot page emit, no DB write here).
  Retry scope: unchanged host machinery — the host applies
  max(poll-interval, 60s) per attempt until the claim attempt budget
  (`workflow.DefaultClaimMaxAttempts`) is spent, then dead-letters.
- No new overlap: retries were already possible in the host; this fix
  lets retryable pages reach the retry path instead of dying at
  validation. Duplicate/retry idempotency is host-owned and untouched.
- Live QA before: 6/6 scanner_worker claims `failed_terminal` with
  `invalid_result: retryable status requires retry_after_seconds`, zero
  facts committed. After (unit proof): the retryable page passes the
  SDK `ValidateResult`; terminal pages still carry zero delay.

## Proof

- `TestRunnerRetryableFailureCarriesRetryAfter` failed pre-fix
  (`retry_after_seconds = 0`) and passes now, including SDK
  `ValidateResult` acceptance of the emitted page.
- `TestRunnerOversizeFactFailsTerminal` pins the terminal side
  (zero delay).
- Component suite ok (7.8s); race run in the pre-push floor.

No-Regression Evidence (retry-after 9591): component suite 7.781s with
the fix vs 7.7s-class baseline on clean main; no queue, lease, batch,
or worker-count change; retry pacing bounded by the 60s request and the
host attempt budget. No load delta is claimed because no throughput
path changed.

No-Observability-Change (retry-after 9591): no metric, span, log, or
status field added, renamed, or removed. Retryable pages are observable
through the existing `component extension result` log line
(`extension_status.retry_after_seconds`,
`extension_status.retry_after_requested_seconds`) and the existing
`failure_class` values; the `invalid_result` terminal for retryables
should disappear from that line.
