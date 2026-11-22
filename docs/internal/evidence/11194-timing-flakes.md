# Evidence: three timing flakes from CI run 44973068482 (#11194)

Records the performance, concurrency and observability evidence for three
fixes to timing-sensitive tests and one shutdown guard.

## Change shape

- `retryTransientAck` in `go/internal/reducer` checks the context before it
  arms its backoff timer and again when the timer fires. A cancelled context
  can no longer start another ack attempt. The ack runs with the caller's
  context, so the old post-cancel retry could not succeed; it failed as a
  non-transient error and surfaced as `ack_failed` plus a run error.
- The cutover claim-mix test meters its workers with permits so a starved test
  goroutine cannot leave a state without a decision.
- The Ifa `killworker_repo_dependency` cell waits for the repo-dependency
  partition lease before it captures the pre-kill snapshot, instead of reading
  once at an arbitrary instant.

No worker count, queue, lease, retry budget, batch size or claim path changes.

## Performance Evidence:

Baseline and after use the same inputs: the focused reducer tests and the
cutover test on one 16-core host at load near 30. The guard adds one
`ctx.Err()` read per transient ack retry and nothing on the success path. The
lease wait adds up to four `psql` polls per second for the wait budget (default
120 s) on the empty-census path only. Cutover test wall time is unchanged at
about 5 s for 100 runs under `-race`. No database or graph backend is involved
in the Go tests; the live cell runs against the pinned Postgres of the Ifa
gate.

## No-Regression Evidence:

- Ack test `TestRetryTransientAckCancelledContextNeverRetries`: fails on the
  base with `run 1: ack calls = 3, want 1`; passes after. Reviewer ran
  `-race -count=300` (exit 0, 74.6 s); the 2,000-run `-race` result is in the PR
  body.
- Cutover `TestCutoverConcurrentClaimMix`: `-race -count=100` exit 0. A 300 ms
  stall injected after the permits are released fails the base design with
  `timed out waiting for restored-state calls: workers stalled` and the new one
  passes. Removing the first revoke makes the new test time out, so its
  per-state gate is not vacuous.
- Ifa self test `scripts/test-verify-ifa-fault-injection.sh`: exit 0; the base
  one-shot capture with the new call site fails it.
- Terminal behavior: a cancelled ack now ends as `errAckRetryInterrupted`,
  recorded as `ack_outcome_unknown`; the claim stays leased and is reclaimed at
  lease expiry. No work is acked twice or lost.

## Observability Evidence:

No-Observability-Change: no metric, span, log key or status field is added. The
guard routes into the existing `ack_outcome_unknown` status and log. An
operator who saw spurious `ack_failed` on shutdown sees `ack_outcome_unknown`
instead, which is the accurate outcome.
