# #8084 GitHub Discovery Retry Evidence

Issue #8084: one GitHub fault ended the whole ingester, which also hosts the
projector. #8109 (for #8076) retried a 5xx page. A transport error, a body
that died on a 200, a 429, a rate-limited 403, and the GitHub App token mint
were still final. This note covers phase 1 of the change: the
`go/internal/collector/repo/git/rest` retry helper, the typed retryable error, and the selector-owned counter.
The collector loop that waits `RetryAfter` and the end-to-end process proof
come in phase 2 and are not claimed here.

No-Regression Evidence: the retry path adds no steady-state cost and bounds
the worst case. Baseline (origin/main 692a952ee5): one request per page and
one mint request per token resolution, fatal on any transport error, decode
error, 4xx, or mint fault; a 5xx page retried up to 4 times. After (this
branch): identical single request when GitHub answers 2xx. A retryable fault
makes at most 4 attempts per call with 1 s, 2 s, 4 s waits spread over 75% to
125%, so a 5xx or transport fault costs at most 4 x 15 s request timeouts plus
about 8.75 s of waits, about 69 s per page. A rate-limit hint of at most 60 s
replaces the backoff, so a call that is rate limited on every attempt waits at
most 3 x 60 s plus 4 request times. A longer or absent hint ends the call at
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
