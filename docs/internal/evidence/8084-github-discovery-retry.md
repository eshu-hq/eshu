# #8084 GitHub Discovery Retry Evidence

Issue #8084: one GitHub fault ended the whole ingester, which also hosts the
projector. #8109 (for #8076) retried a 5xx page. A transport error, a body
that died on a 200, a 429, a rate-limited 403, and the GitHub App token mint
were still final. This note covers both parts of the change: the
`go/internal/collector/repo/git/rest` retry helper, the typed retryable error,
and the selector-owned counter (part 1), and the `collector.Service.Run`
branch that skips a failed discovery cycle instead of exiting, with the
process-level test (part 2).

No-Regression Evidence: the retry path adds no steady-state cost and bounds
the worst case. Baseline (origin/main 6081695c63): one request per page and
one mint request per token resolution, fatal on any transport error, decode
error, 4xx, or mint fault; a 5xx page retried up to 4 times. After (this
branch): identical single request when GitHub answers 2xx. A retryable fault
makes at most 4 attempts per call with 1 s, 2 s, 4 s waits spread over 75% to
125%, so a 5xx or transport fault costs at most 4 x 15 s request timeouts plus
about 8.75 s of waits, about 69 s per page. A rate-limit hint of at most 60 s
replaces the backoff, so a call that is rate limited on every attempt waits at
most 3 x 60 s plus 4 request times: the in-call bound is 240 s per call
(3 x 60 s + 4 x 15 s). A longer or absent hint ends the call at
once with the wait handed to the collector loop. Backend: go1.26.9
darwin/arm64, fake `httptest` GitHub (no live calls). Input shape: reset,
client timeout, EOF mid-body, HTTP/2 stream reset, 429, rate-limited 403,
persistent 504, 401/403/404/422/3xx, and a two-page listing with a fault on
page 2. Measured: focused retry tests PASS (injected sleep and clock, no real
waiting); `go test -race -count=1 ./internal/collector/repo/git/...
./internal/collector/sdk/... ./internal/telemetry/...` PASS, the git package
in 112.8 s. Terminal queue and row counts are unchanged at 0 for this path
(no storage, graph, or queue writes). Safe because every retried call is
idempotent (GET listing, POST token mint that issues an independent token),
retries are sequential (no new concurrency, no worker, batch, or lease
change), 401, other 4xx, and invalid JSON on a full body stay final,
cancellation aborts a request and a backoff wait and is never typed retryable,
and a permanent fault still ends the process: the selector turns the 20th
consecutive retryable cycle into a fatal error (`sdk.MaxConsecutiveTransportFailures`).
Conflict domain: the GitHub API of one org, driven one cycle at a time by one
selector; the only shared state is the selector's own failure counter.

Observability Evidence: `eshu_dp_github_api_attempts_total` (`operation`,
`failure_class`, `outcome`) and `eshu_dp_github_api_retries_total`
(`operation`, `failure_class`) count each attempt and retry. The
`github.api_retry` span event and the `GitHub API call retrying` WARN log
(`operation`, `failure_class`, `attempt`, `status`, `backoff_seconds`,
`retryable`) mark each retry.
`TestGitHubAPIRetrySignalsReachTheOperator` asserts all three from a real
selection cycle. The counters are documented in
docs/public/reference/telemetry/metrics.md and the coverage row lives in
docs/public/observability/telemetry-coverage.md.

## Before and after at the process level

Before: the issue records eleven org-listing exits of the ingester process in
about 26 hours in the QA environment. Each one ended the collector and, through
`compositeRunner`, cancelled the projector that runs in the same process. One
outage cluster was 5 consecutive failing discoveries inside 2 min 17 s, then
recovery; another was 3 failures inside 34 s, then a clean start. Before this
change a retryable `Next` error, whatever its class, took the fatal return in
`Service.Run`.

After: `TestDiscoveryOutageDoesNotEndTheIngesterProcess` runs the real
`GitSource`, `NativeRepositorySelector` (githubOrg mode, App token mint),
`collector.Service`, and `compositeRunner` against one fake GitHub that
answers 504 for N consecutive cycles. Result in the test: for N = 5 and N = 3
the run returns only at shutdown with a nil error, the projector context is
never cancelled, the skipped-cycle counter equals N, the consecutive-failure
gauge is 0 after the first good listing, and the listing saw N x 4 failing
requests (4 attempts per cycle). For N = 20 the run returns a non-retryable
error that names the class and `20 consecutive`, after 19 skipped cycles.
Mutating `Service.Run` to treat the error as fatal again turns all three red.

What this is not: a model and test result against a fake provider, with the
in-call sleep and the loop wait injected. It is not a measurement of a deployed
ingester, and it does not show the real wall time of an outage: with no
provider hint the between-cycle waits add up to roughly 17 to 40 minutes before
the 20th failure ends the process (5 + 10 + 20 + 40 + 16 x 60 s of waiting plus
up to about 69 s of in-call retry per cycle, before jitter). The production
exit count must be re-read after a rollout; this note does not claim zero.

Concurrency and conflict domain are unchanged: one selector, driven one cycle
at a time; the loop adds a wait, not a goroutine or a lock. Worker counts,
batch sizes, and leases are untouched.

Observability Evidence (loop): `eshu_dp_collector_discovery_skipped_cycles_total`
(`collector_kind`, `failure_class`), `eshu_dp_collector_discovery_consecutive_failures`
(`collector_kind`), the WARN line `collector discovery retryable; skipping
cycle` (`attempt`, `failure_class`, `consecutive_failures`,
`retry_after_seconds`, `retryable`), the `collector discovery failed` log, and
the error status on the `scope.assign` span. `TestServiceRunSkipSignalsReachTheOperator`,
`TestDiscoveryConsecutiveFailuresGaugeFollowsTheSelector`, and
`TestDiscoverRepositoriesRecordsAFailedSelection` assert them.

## Webhook sync path (review r1 F1, ruling 3)

The first part of this change counted only the native selector's failures.
The webhook-trigger selector also mints a token while it syncs the claimed
repositories, and a retryable mint failure there was returned as a skippable
error with no counter: a 1 s skip loop, four mint POSTs per second per shard,
and one batch of claimed triggers marked `sync_git_failed` (terminal) per
cycle. One `rest.Failures` per process is now shared by both selectors and both
wirings, and counts per operation (`list`, `mint`), so a good mint cannot hide
a dead listing and a dead mint cannot hide behind a good listing.

Bound on trigger survival, from source: the lease window defaults to 15
minutes and a claim row is attempted at most 3 times
(`defaultWebhookTriggerClaimLeaseWindow`, `defaultWebhookTriggerMaxClaimAttempts`
in `webhook_trigger_selector.go`). A row left `claimed` by skipped cycles is
requeued after the first and second window and moved to `failed` with
`claim_lease_exhausted` after the third, so a claimed trigger survives an
outage of about 45 minutes with the defaults. A longer outage still ends it
failed, under a class that names a poison row rather than the outage. The
signals are `eshu_dp_webhook_trigger_claim_reaps_total{outcome="exhausted"}`,
the `reaped expired webhook trigger claims` log, and the stale-claims gauge.
Base marked every claimed trigger `sync_git_failed` on the first blip, so this
is strictly better but not lossless. In priority mode the scheduled native
sync picks the repository up anyway; in webhook-only mode the change waits for
the next push to it.

Results in tests, not deployed measurements:

- A webhook selector with a real mint answering 502 through `rest.Do` ends
  fatal on the 20th cycle in both the priority wiring and the webhook-only
  wiring. The 19 skip waits are the 5 s doubling backoff capped at 60 s, the
  fake GitHub sees 4 mint attempts for each of the 20 cycles, and the claimed
  triggers are never marked failed.
- A native `list` failure, then a good non-empty webhook cycle, leaves the
  `list` count unchanged; a native nil return resets both operations.
- A sync failure that arrives together with a failing `MarkTriggersFailed`
  write stays fatal; a webhook selector with no counter wired fails closed.
- `cmd/ingester` and `cmd/collector-git` tests assert both selectors hold the
  same `*rest.Failures`.

Figures to keep: the in-call bound is 240 s per call under three consecutive
60 s hints (3 x 60 s + 4 x 15 s request timeouts); the between-cycle backoff
has a 60 s base cap and is spread over 75% to 125% after the cap, so the wait
at the ceiling is 45 s to 75 s; a provider that keeps sending a one-hour hint
can hold the loop for up to 20 x 1 h before the cap turns fatal, which is
provider-instructed and visible on every cycle in the WARN line and the
consecutive-failure gauge.

Still not claimed: any deployed measurement. The tests run against a fake
provider with the sleep and loop wait injected, and with the identity jitter
where a schedule is asserted. A rollout must re-read the real exit count, the
skipped-cycle counter, and the consecutive-failure gauge.
