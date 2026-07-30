# Code-Call Self-Loop Tally Evidence (#5332)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. Content is
unchanged from the original section.

## Code-call self-loop tally (#5332)

`recordCodeCallSelfLoopWritten` (`code_call_materialization_extract.go`) runs
once per `extractCodeCallRowsWithIndex` call, after row extraction, sorting,
and dedup. For each materialized row it compares `caller_entity_id` and
`callee_entity_id`; a match is genuine recursion (the caller and callee
resolve to the same entity), not the #5332 parser defect the Dart AST rewrite
removed (a declaration self-read as a call to itself — see
`go/internal/parser/dart/calls.go`). Row writing is unchanged: every row,
self-loop or not, is still returned and written. The pass only tallies
self-loops per `lang` (carried onto each row by
`appendCodeCallRow`/`copyOptionalCodeCallField` in
`code_call_materialization_index_rows.go`) and logs the tally once if any were
found.

Performance Evidence: the added work is a single extra `range` over the
already-materialized `rows` slice — a map increment (`tally[lang]++`) and one
string comparison per row, no allocation on the zero-self-loop path (the
common case; `tally` and `total` are only used if `total > 0`) — plus at most
one `slog.Info` call for the whole batch, not per row. It runs after the
existing sort, so it does not change algorithmic complexity: extraction stays
O(n log n) in row count, dominated by the pre-existing `sort.Slice` a few
lines above. It adds no Cypher, no graph write, no Postgres query, and no
additional pass over the source `envelopes`.

No-Regression Evidence: baseline (before this change) had no counter and no
log line on this path; after, every row is still returned unchanged
(`len(rows)` and row contents are identical — proven by
`TestExtractCodeCallRowsWritesGenuineSelfLoop`, which asserts the self-loop
row is present with `caller_entity_id == callee_entity_id` and the `lang`
field intact) and one bounded map-plus-log pass runs afterward. Input shape:
one `code_call` row per resolved call edge, the same shape
`extractCodeCallRowsWithIndex` already produces per repository batch; the
tally is O(rows) with a small (language-cardinality) map, not O(rows^2) or
graph-sized, so it adds no measurable per-row cost at any corpus scale. `go
test ./internal/reducer -run
'CodeCallRows|SelfLoop|Instantiates|CodeCall' -count=1` passes.

Observability Evidence: `recordCodeCallSelfLoopWritten` logs one
`slog.Info("code call self loop written", "event",
"code_call_self_loop_written", "total", <count>, "by_lang", <map[lang]count>)`
line per materialization pass that contains at least one self-loop row (no
log line when `total == 0`). An operator greps
`code_call_self_loop_written` to see recursion-edge volume per source
language over time; a sudden spike in one language's count — or a language
that starts appearing that previously had none — flags a future parser
self-loop regression in that language before it silently changes fan-in/call-
count graph signals corpus-wide (the failure class #5332 fixed for Dart).
`TestExtractCodeCallRowsLogsSelfLoopTallyByLanguage` proves the log entry's
shape (`event`, `total`, `by_lang` keyed by language) end to end through the
real extraction path, not a re-implementation.
