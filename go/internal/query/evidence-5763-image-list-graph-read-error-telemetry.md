# Evidence: image-list telemetry on the graph-read-error branch (#5763)

Hot-file evidence for `images.go`'s `listImages` handler. The
`verify-performance-evidence.sh` gate classifies `images.go` as a changed hot
file for this branch by content, not by location: `is_hot_path_by_content`
(`scripts/verify-performance-evidence.sh:84-94`) matches the `MATCH` in
`imageListCypher` (`images.go:31`). `go/internal/query` carries no
`is_hot_path_by_location` arm (`scripts/verify-performance-evidence.sh:61-82`)
— that function's list is `storage/cypher`, `storage/neo4j`,
`storage/postgres`, `collector`, `graph`, `projector`, `reducer`, `queue`,
`runtime`, `workflow`, plus four `go/cmd/*` arms, and `go/internal/*` appears
only in the separate `is_go_runtime_file` *eligibility* predicate
(`scripts/verify-performance-evidence.sh:56`). Because `images.go` is hot by
content, the gate also required evidence when two statements were added to
its `WriteGraphReadError` guard branch. The sibling telemetry files in this
same directory, `images_telemetry.go` and `tag_history_telemetry.go`, do not
match the content probe and are therefore not hot files under this gate; a
future edit to either does not by itself trigger this evidence requirement.
This note carries the required `No-Regression Evidence` and `Observability
Evidence` markers for the `images.go` change.

## What changed

`images.go`'s `listImages` handler already recorded the image-list duration
histogram and error counter on every other outcome (`ok`,
`invalid_request`, `unsupported_capability`, the `h.Neo4j == nil` guard's
`backend_unavailable`, and `query_error`) except one: the
`WriteGraphReadError` guard branch, reached when `h.Neo4j.Run` returns
`ErrGraphUnavailable` (503) or `ErrGraphReadDeadline` (504), returned without
recording either instrument. Two statements were added on that branch:

```go
if WriteGraphReadError(w, r, err, imageListCapability) {
    recordImageListError(r.Context(), "backend_unavailable")
    recordImageListDuration(r.Context(), start, "backend_unavailable")
    return
}
```

A doc comment was also added immediately above the guard (comment-only,
`is_comment_only_change`-eligible on its own, but landing alongside the two
statement additions above in the same hot file) explaining that this
handler-level outcome label deliberately folds `ErrGraphUnavailable` and
`ErrGraphReadDeadline` into a single `backend_unavailable` outcome, matching
`tag_history.go`'s identical (untouched, on `origin/main`) guard. The comment
also points at the lower-level `eshu_dp_neo4j_query_duration_seconds{outcome="deadline"}`
vs. `{outcome="unavailable"}` instrument (`recordGraphReadTelemetry` in
`neo4j_read_policy.go`), and is careful to say that instrument records the
same outage/deadline split on every bounded graph read process-wide, with no
route dimension — it cannot attribute a given outage or deadline to this
route specifically. Per-request attribution for this route lives on the
`neo4j.query` span's `telemetry.SpanAttrGraphReadOutcome` attribute
(`neo4j_read_policy.go:347`) instead.

No-Regression Evidence: this is not a performance change: no Cypher statement, graph write, query
plan, loop, or retry/backoff behavior changed. The two added statements sit
on an already-failing response path — the handler has already decided to
return `503`/`504` via `WriteGraphReadError` before either statement runs —
so they add two in-process metric `Record` calls (an `Int64Counter.Add` and a
`Float64Histogram.Record`, both no-op-safe on a nil instrument per
`initImageQueryInstruments`'s doc comment) to a branch that was already
about to write the HTTP response and return. No new round trip, lock,
transaction, or graph call is introduced; `h.Neo4j.Run` is called exactly
once per request either way, unchanged from before this branch. The read
path's row shape, ordering, limit/offset handling, and truncation metadata
are untouched.

Baseline = `WriteGraphReadError(...)` returns `true` and the handler returns
immediately with zero telemetry records. After = the same control flow, plus
two records under the existing `backend_unavailable` outcome label the
`h.Neo4j == nil` guard already uses two branches above. Verified with
`cd go && go test ./internal/query -run
'TestImageHandlerGraphReadErrorRecordsBackendUnavailableOutcome' -v -count=1
-race` (new regression, RED before the fix — confirmed by reverting the two
added statements and re-running: `errors counter reason=backend_unavailable =
0, want 1`) and `cd go && go test ./internal/query -count=1 -race` (full
package, unaffected).

Observability Evidence: two existing instruments — `eshu_dp_query_image_list_duration_seconds`
(`imageListDuration`, a `Float64Histogram`) and
`eshu_dp_query_image_list_errors_total` (`imageListErrors`, an
`Int64Counter`), both registered by `initImageQueryInstruments` in
`images_telemetry.go` — now also fire on the `WriteGraphReadError` guard
branch, labeled `outcome="backend_unavailable"` /
`reason="backend_unavailable"`. No new metric, label, attribute, span, or log
key is added; this only closes a gap where a real graph outage or read
deadline on `GET /api/v0/images` produced a correct `503`/`504` HTTP response
but zero datapoints on either instrument. This PR also adds the
"Container images (bounded list)" row for this handler to
`docs/public/observability/telemetry-coverage.md`, which names both
instruments; at `origin/main` that file has no row for `images.go` at all.

`TestImageHandlerGraphReadErrorRecordsBackendUnavailableOutcome`
(`images_telemetry_test.go`) pins this: it installs a `fakeImageGraphReader`
that returns `ErrGraphUnavailable`, asserts the handler responded `503`,
asserts `fakeReader.lastCypher != ""` (proving `h.Neo4j.Run` — and therefore
the graph-read-error branch, not the `h.Neo4j == nil` branch above it — was
actually exercised), and then asserts both instruments recorded exactly one
`backend_unavailable` datapoint via `collectImageMetrics`.

Verification run this session: `cd go && go test ./internal/query -run
'TestImageHandlerGraphReadErrorRecordsBackendUnavailableOutcome' -v -count=1
-race` and `cd go && go test ./internal/query -count=1 -race`.
