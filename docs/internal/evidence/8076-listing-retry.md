# #8076 Org Listing 5xx Retry Evidence

No-Regression Evidence: the retry path adds no steady-state cost and bounds
the worst case. Baseline (origin/main 1bd18531): one 15s-timeout request per
page, fatal on the first 5xx. After (this branch): identical single request
when GitHub answers 2xx; on 5xx at most 4 attempts with 1s, 2s, 4s backoff,
so the worst case per page is 4 x 15s timeouts + 7s waits = 67s, after which
the page fails with `status 502 after 4 attempts` exactly as before. Backend:
go1.26.2 linux/amd64, fake `httptest` GitHub (no live calls). Input shape:
502-once-then-valid, persistent-500, 401, and paged valid listings. Measured:
focused retry tests PASS in 0.02s (injected clock, no real waiting);
`go test -count=1 ./internal/collector/repo/git/` PASS in 83s;
`verify-dirgate.sh --all` PASS; grandfather mirror 9/9. Terminal queue and
row counts are unchanged at 0 for this path (no storage, graph, or queue
writes). Safe because retries are GET-only (idempotent), sequential (no new
concurrency), 4xx/transport/decode stay final, and context cancellation still
aborts both in-flight requests and backoff waits.

No-Observability-Change: this change emits no new metric, span, log key, or
status row. Terminal failures propagate to the collector cycle outcome
already covered by `eshu_dp_workflow_claim_run_duration_seconds` and
`eshu_dp_workflow_claim_facts_emitted_total`; the per-stage row lives in
docs/public/observability/telemetry-coverage.md next to this change.
