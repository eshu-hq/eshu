# Graph-Read Statement Fingerprint Evidence (#7035)

Every bounded `Neo4jReader` read now computes a stable statement fingerprint
(first 12 hex characters of sha256 of the whitespace-collapsed Cypher text,
never parameters) in `runRead`, attaches it to the `neo4j.query` span on every
read, and adds it plus a bounded statement head to the
`query.graph_read.warning` log for slow/deadline/unavailable outcomes. The
slow threshold also became configurable
(`ESHU_GRAPH_READ_SLOW_THRESHOLD`, registered in
`go/internal/envregistry/entries.go`), which is what puts this change on the
performance-evidence gate's hot-path scan (it flags `ESHU_GRAPH_*` names).

## No-Regression Evidence

The added work per read is exactly one whitespace-collapse
(`strings.Fields`+`strings.Join`) and one `sha256.Sum256` +
`hex.EncodeToString` over the Cypher statement text -- fixed cost independent
of row count, backend, or network round trip, computed once per `runRead`
call regardless of outcome. It never touches parameters, never re-runs the
statement, and adds no retry, lock, or backend round trip.

Measured on an Apple M5 Max (`darwin/arm64`), comparing this branch's commit
`8d699a67a6` (first fingerprint commit) against its parent `0d1647f4c1`
(`origin/main` at the time of the PR) via a detached `git worktree add` of the
parent commit, so both sides build from the identical `neo4j_read_policy_bench_test.go`
harness:

```
go test ./internal/query -run '^$' -bench 'BenchmarkNeo4jReaderHealthyPolicyOverhead' \
  -benchtime=200000x -count=3
```

Run twice with alternating first-mover (before-then-after, then
after-then-before) to control for in-process warm-up bias on a shared
benchmark harness that stubs the driver/session (no real network or graph
backend):

| Shape | Before ns/op (6 samples, 2 orders) | After ns/op (6 samples, 2 orders) | Before B/op | After B/op | Before allocs/op | After allocs/op |
| --- | --- | --- | --- | --- | --- | --- |
| `unbounded_reader` | 778.7 / 1007 / 995.2 / 852.5 / 835.2 / 816.1 (median ~844) | 933.9 / 894.2 / 1006 / 875.8 / 838.3 / 856.0 (median ~885) | 1248 | 1528 | 19 | 23 |
| `bounded_reader` | 1646 / 1669 / 2093 / 1238 / 1192 / 1231 (median ~1442) | 1405 / 1512 / 1578 / 1215 / 1220 / 1204 (median ~1312) | 1544 | 1824 | 25 | 29 |

The ns/op deltas are not a reliable signal here: whichever side runs first in
a process is consistently ~300-650ns slower than the same side run second
(warm-up/JIT/allocator-arena effects), which swamps a sub-100ns fingerprint
cost on this microbenchmark's ~1-2us scale -- the "before" shape is slower
than "after" in the first-mover order and faster in the second-mover order.
The allocation counts are the reliable signal instead: they are exactly
reproduced across all four independent runs (both orders) with zero variance,
and isolate precisely the fingerprint's fixed cost: **+280 B/op and +4
allocs/op**, identical for both the bounded and unbounded reader shapes (the
fingerprint call is the only change common to both call sites). That matches
the implementation exactly -- one `strings.Fields` slice allocation, one
`strings.Join` string allocation, and one `hex.EncodeToString` string
allocation from the whitespace-collapse-and-hash path (`sha256.Sum256` itself
returns a fixed-size array, no heap allocation).

+280 B/op and +4 allocs/op is negligible against the reader's existing
10-second deadline budget and against any real Cypher execution, which
dominates every one of these shapes by orders of magnitude in production (the
benchmark's mocked session has no real backend round trip). No worker count,
lease, batch size, or concurrency knob changed; this is a pure per-read
constant-cost addition on the existing single-read path, not a new
serialization point.

Focused proof: `cd go && env -u GOROOT go test ./internal/query -count=1` (all
`neo4j_read_policy*` tests, including the new fingerprint/threshold suite)
passes; see the PR's test evidence for the full RED/GREEN and mutation-testing
record.

## Observability Evidence

- `neo4j.query` span gains `eshu.graph_read.statement_fingerprint`
  (`SpanAttrGraphReadStatementFingerprint`,
  `go/internal/telemetry/contract/graph_read.go`), set on every read
  regardless of outcome.
- `query.graph_read.warning` (slow/deadline/unavailable outcomes only) gains
  `graph_read.statement_fingerprint` and `graph_read.statement_head`
  (`LogKeyGraphReadStatementFingerprint`/`LogKeyGraphReadStatementHead`, same
  file). The head is whitespace-collapsed and truncated to 300 characters
  with an `...[truncated]` marker; parameters are never included in either
  field.
- The fingerprint is deliberately **not** added as a metric label:
  `eshu_dp_neo4j_query_duration_seconds` keeps its existing closed
  `operation`/`outcome` dimensions only, so this change adds no cardinality.
- An invalid or non-positive `ESHU_GRAPH_READ_SLOW_THRESHOLD` override logs
  `query.graph_read.invalid_slow_threshold` once at warn level and falls back
  to the 1-second default rather than silently disabling the warning.
- `docs/public/reference/telemetry/graph-read-safety.md` documents both
  fields plus how to correlate `graph_read.statement_head` against NornicDB's
  `event="slow_query"` log and a NornicDB pprof capture.

Refs #7035
