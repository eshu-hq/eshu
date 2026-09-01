# Cross-Repo Call Resolver Benchmark Evidence (#3487 / #3624)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap.

The measurements, commands, and numbers below are verbatim from the original
section. One closing paragraph did not come across: it explained why the
section stayed in the README rather than moving to `domain-catalog.md`, which
would contradict itself here. Its substance still applies and now lives in the
README beside the table -- `go/internal/accuracygate/golden_gate_test.go`
(`TestAccuracyResolverMatrixMatchesPublishedDoc`) reads that file directly and
parses the resolver coverage table, so the table must resolve at that path and
stays there.

## Dedicated resolver dispatch cost (#3487)

The swift/javascript/jsx resolvers and the shared receiver-method index add one
map insertion per indexed function during index construction and one O(1) map
lookup per receiver-typed call before the repo-fallback stage; the dispatch order
is otherwise unchanged.

- No-Regression Evidence: `BenchmarkExtractCodeCallRowsLargeJavaScriptDynamicCalls`
  (`go test ./internal/reducer/ -bench ... -benchmem`), Go 1.x on darwin/arm64,
  large synthetic JavaScript code-call corpus exercising `ExtractCodeCallRows`
  (full index build + resolution). Baseline at `b491df69` (pre-#3487):
  ~9.9–11.8 ms/op, ~1.66 MB/op, 30206–30211 allocs/op. After this change:
  ~11.2–12.0 ms/op median (5 samples), ~1.65 MB/op, 30207–30209 allocs/op.
  Allocation count and bytes/op are flat (slightly lower); ns/op ranges overlap
  within machine noise on a shared host. The added index is O(functions) to build
  and O(1) per call, so there is no algorithmic regression.
- Observability Evidence: resolution provenance is the operator-facing signal for
  this path. The new swift/javascript/jsx resolvers record
  `codeprovenance.MethodTypeInferred` on resolved edges (and leave the edge
  unresolved, with no provenance, when the receiver type is ambiguous or absent),
  so resolved-vs-unresolved cross-repo calls remain visible through the existing
  `resolution_method` provenance on materialized code-call rows without adding a
  new metric or span.

## Repository-wide import-path cache (#3624)

Performance Evidence: #3624 cached the repository-wide normalized import-path
set once per code-call extraction instead of rebuilding it for every unresolved
JavaScript or Python call. On Linux amd64 with Go 1.26.2, the retained worst-case
scope contained 12,403 input envelopes (one repository and 12,402 files), 46,424
functions, and 1,123,223 generic calls. The completed baseline at `b49d9655d`
spent 3,693.17 seconds in extraction; the prototype based on current-main commit
`35443fd4d` completed the same extraction in 35.02 seconds. The candidate
produced 76,832 rows and 61,603 intents. A comparison against the persisted
baseline intents found zero missing, unexpected, or identity/payload-mismatched
rows. The focused darwin/arm64 benchmark with 5,001 repository paths and 1,000
unresolved calls dropped from 433-459 ms and about 681 MB/op to 7.27-7.66 ms and
about 5.8 MB/op. Classification: handler win. The proof does not claim a new
full-corpus queue-zero time.

No-Observability-Change: the cache changes only in-memory call resolution. The
existing `code call materialization completed` log still reports
`extract_duration_seconds`, `code_call_row_count`, and `intent_row_count`, which
show the handler cost and output cardinality without a new metric, span, label,
queue, or runtime setting.
