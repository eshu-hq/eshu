# Impact Findings 500: Missing Failure Signal (#7546)

## What was reported

One HTTP 500 from `GET /api/v0/supply-chain/impact/findings` on ops-qa left no
attributable log line. This note records what the logs showed, what is not
known, and what signal the change adds. It does not claim a cause.

## What the pod log showed

Read-only log analysis of the API pod `eshu-api-75c47bdf46-7fmss` (single
replica, 0 restarts, image `sha-101cd4f`):

- 92 traces ran the `impact_findings_query` stage.
- 91 of them ran all five stages (`impact_findings_query`,
  `cloud_runtime_evidence`, `kubernetes_runtime_evidence`, `runtime_context`,
  `readiness_snapshot`).
- 1 trace, `b7c4236f15f6024a77829b79b788bc96`, ran only the first stage. It
  started at 10:53:20.416 UTC and completed in 4.9 ms with `rows_fetched=0`;
  nothing followed. The same repository answered 200 about 1.25 s later.

That shape is exactly what `listImpactFindings` does when
`ImpactFindings.ListSupplyChainImpactFindings` returns an error: it calls
`querycontract.WriteError(w, 500, err.Error())` and logs nothing, and the
stage completion event carried no `error` attribute, so a failed read and an
empty page logged identically. The correlation of this trace with the HTTP 500
is inferred from that shape, not proven.

## The cause is not established

No log, span, or counter recorded the error text for that request. The
candidate explanations (for example a non-deadline guarded-reader borrow
failure, which `querycontract` does not map to a 503/504 verdict and which
therefore stays the handler's 500) are hypotheses. Nothing in this change or
note selects one.

## Supporting observations, not a cause

Read from the same pod's cumulative Prometheus endpoint on 2026-10-03 (about 12:08 UTC), reported by the
engineer who scraped it, not re-derived by a gate:

- `eshu_dp_api_request_errors_total{route="GET /api/v0/supply-chain/impact/findings",status_class="5xx"}`
  was 1 for the pod's 11 h lifetime, and was 1 again after the 200-call run below. No other route had a 5xx.
- `eshu_dp_postgres_reader_stage_duration_seconds_count{stage="reader_borrow",outcome="error"}` was 1 over the
  same lifetime (and unchanged after the run). The counters carry no timestamps, so they cannot show that
  the borrow error and the 500 are the same request. The matching counts and the 4.9 ms stage duration make a
  non-timeout reader-borrow failure a candidate; it is not established.
- The replica's `postgresql` log (errors only) holds no line between 10:50:00 and 10:54:30 UTC, so no
  replica-side error is recorded around 10:53:20. The log window before 10:54:30 is absent, so this does not
  rule out a connection that never reached a backend.
- Reproduction attempt: 200 serial `limit=1` calls over 10 fresh port-forwards against six anchors returned
  200 times HTTP 200 (slowest 0.334 s), with both counters unchanged. Together with the 91 earlier calls that
  had no repeat and 30 further calls in the issue, the 500 has not recurred in about 320 calls. This is a
  lower bound on rarity, not a rate.

A second, longer run (reported by the engineer who ran it: 1,500 serial `limit=1` calls, 6 anchors, fresh
port-forward every 50 calls, a metrics scrape every 20 calls, 12:11:39 to 12:21:37 UTC) did produce HTTP 500s, but
of a different class than the one this issue reports:

- 4 of 1,500 calls returned 500, each after 2.09 s (calls 163, 164, 165 and 325); the other 1,496 returned 200
  (client-measured). The API's 5xx counter for the route went from 1 to 5, matching the four client-observed
  responses.
- `reader_replay` outcome=deadline rose by 7 over the run, all of it in the two 20-call windows that hold those
  500s (calls 161 to 180: +5; calls 321 to 340: +2), and `reader_borrow` outcome=error rose by 0. A 2.09 s duration
  sits just above the 2 s replay fence timeout, so these are consistent with replay-fence timeouts, the class #7548
  covers; that is an inference from the counter window and the duration, not a per-request attribution.
- The 7 deadline increments exceed the 4 observed 500s. The other 3 are unexplained: they were not 500s, so
  candidates are a readiness read that degraded to a 200 (call 166 returned 200 after 2.087 s), a call by another
  client, or a different route. Not checked.
- The original failure is the issue's client-measured 0.096 s total, while its findings stage took 4.9 ms in the pod
  log, which leaves about 91 ms outside the stage (not attributed). It is a different duration class from the 2.09 s
  cases above.
- The API pod log was empty when read after the run (0 lines over 3 h; cause not established), so the per-trace
  stage join for this run is NOT_CHECKED.

So the original 0.096 s 500 remains unreproduced (about 1,700 calls since); the reader_borrow hypothesis above is
neither supported nor ruled out by this run.

## What the change makes attributable

The wire behavior (status codes, response bodies) is unchanged. On each
handler-owned 5xx branch of the route (findings read, cloud-runtime probe,
Kubernetes-runtime probe non-graph error, runtime-context probe non-graph
error) the next occurrence now yields:

- one ERROR `supply_chain_query.stage_failed` log line with `stage`,
  `repo_id`, `operation`, `duration_seconds`, and `error` (text cut to 256
  bytes);
- closed-set `error_site` (`reader_stale`, `reader_unavailable`, `other`) and
  `error_cause` (`deadline_exceeded`, `canceled`, `conn_done`, `eof`,
  `conn_refused`, `conn_reset`, `net_timeout`, `sqlstate_<class>`, `unknown`),
  derived from the error chain with `errors.Is` and `errors.As` only, which
  separates causes that share the fixed text
  `PostgreSQL reader connection unavailable`;
- an `error` boolean on the `impact_findings_query` completion event, like the
  other stages;
- the error recorded on the handler span with Error status, so the trace is
  findable.

Not reachable from the query layer: the writer-side and topology sentinels in
`internal/runtime/postgres` classify as `error_site=other`.

Observability Evidence: `supply_chain_query.stage_failed`,
`supply_chain_query.stage_completed` (`error` attribute), and the handler span
error status, locked by `TestListImpactFindingsLogsFailedStageOnHandlerOwned500`
(one case per 5xx branch, including a 600-byte ASCII and a 600-byte multibyte
error), `TestListImpactFindingsStageCompletionErrorAttributeAndNoFailureOnSuccess`,
`TestListImpactFindingsNilLoggerStillFailsWith500`, and
`TestClassifyReaderFailure` in `go/internal/query/supply/chain`.

No-Regression Evidence: the change adds work only on the error branches (one
log call, one span event, a bounded string cut, and an error-chain walk) plus
one boolean attribute on the `impact_findings_query` completion event that is
already emitted. The success path gains no query, allocation-per-row, or lock.
No before/after timing was measured because the success path does no new work
beyond building one extra `slog.Bool` attribute; no throughput figure is
claimed.
