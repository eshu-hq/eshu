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
