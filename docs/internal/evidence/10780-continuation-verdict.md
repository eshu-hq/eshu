# 10780 - continuation page verdicts in the extension host

A paged collector that refused a continuation (catalogue changed, unreadable
cursor) answered with a retryable or terminal page, and the operator saw a
terminal `invalid_result` instead. This records what changed and the evidence
for it.

## What was wrong

`collectPages` appended every page's facts and statuses and returned the last
call's result with the accumulated facts. A retryable or terminal state must not
carry facts (`validateStateFactAllowance` in the SDK validator), so the assembled
result failed validation. A verdict page without a page envelope also failed
`checkPageEnvelope` ("dropped its page envelope mid-stream"), and a verdict
page could not match page 0's observed_at or declared total.

## What changed

A retryable or terminal continuation page with no facts now ends the stream and
is returned as the result, without the earlier pages' facts or statuses. Nothing
commits. It may omit the page envelope and skips the generation and total pins;
the claim echo and the SDK validator still apply. A failure page that carries
facts, and any other envelope-less mid-stream state, keep failing as before.

Performance Evidence: the loop adds one `isVerdictPage` state switch per page
after the first and one per envelope-less page, no allocation. `go test
./internal/collector/extensionhost -run '^$' -bench BenchmarkCollectPagesReassembly
-benchmem -count=5` (darwin/arm64, go1.26.11, shared laptop, so timing is noisy):
base and branch both report 1677 allocs/op one-shot and 1695 allocs/op for three
pages, with B/op within 0.3%. ns/op varied 210,000-390,000 across runs on both
sides and does not support a timing claim in either direction.

No-Regression Evidence: the happy multi-page path is unchanged and still covered
by `TestCollectPagesReassemblesIdenticalFactSets`; the existing framing checks
(gapless page_index, pinned generation, declared total, cursor rules) are
covered by `TestCollectPagesRejectsOutOfContractPages` and the new
`TestCollectPagesRejectsUnframedNonVerdictPages`. A first-page verdict keeps its
verdict (`TestCollectPagesFirstPageVerdictUnchanged`). No query, queue, lease, or
graph write changed.

Observability Evidence: no metric, label, span, or log key was added. A
continuation verdict now reaches the existing
`eshu_dp_component_extension_results_total` counter, the result log line, and
the retry-after gauge with the satellite's own `failure_class` and state, where
before it counted as `invalid_result`.
