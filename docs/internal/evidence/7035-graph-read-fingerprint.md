# Graph-Read Statement Fingerprint Evidence (#7035)

Every bounded `Neo4jReader` read now computes a stable statement fingerprint
(first 12 hex characters of sha256 of the statement's redacted,
whitespace-collapsed shape -- every numeric and string literal replaced by
`<REDACTED>` (booleans and null are kept), comments
dropped; never parameters or inline literal values) in `runRead`, attaches it
to the `neo4j.query` span on every read, and adds it plus a bounded redacted
statement head to the `query.graph_read.warning` log for slow/deadline/
unavailable outcomes. The redaction is a single-pass scanner in
`go/internal/query/graph/statement`, added after review found that ad-hoc
Cypher submitted through `/api/v0/code/cypher` carries its values as inline
literals that the first cut logged verbatim. The slow threshold also became
configurable (`ESHU_GRAPH_READ_SLOW_THRESHOLD`, registered in
`go/internal/envregistry/entries.go`).

This change reaches the performance-evidence gate's content scan by file
granularity, not because it alters a hot path: the scan matches whole changed
`.go` files, and `entries.go` (an unrelated existing "Heartbeat" description),
`neo4j_read_policy_fingerprint_test.go` and `neo4j_read_policy_telemetry_test.go`
(Cypher strings in test fixtures) each contain a pattern the scan looks for.
The evidence below is therefore the honest measurement for a read-path
change, recorded so the gate has its markers.

## No-Regression Evidence

No-Regression Evidence: the added work per read is one pass of the redacting
scanner (`statement.Redact`, which also collapses whitespace) plus one
`sha256.Sum256` and `hex.EncodeToString` over the result -- fixed cost
independent of row count, backend, or network round trip, computed once per
`runRead` call regardless of outcome. Redaction now runs on every read, not
only the warning path, because the fingerprint hashes the redacted text. It
never touches parameters, never re-runs the statement, and adds no retry, lock,
or backend round trip. The warning path runs the scanner a second time to build
the head; that is off the healthy path.

Measured on an Apple M5 Max (`darwin/arm64`) with detached `git worktree add`
checkouts of `origin/main` (`6ef17e7a47`) and of the previous branch head
(`4ea998f8a5`, the `strings.Fields`+`strings.Join` fingerprint), each built and
run separately, five samples per shape per side, `-count=5`, alternating which
side runs first (two orders, ten samples per side per shape):

```
go test ./internal/query -run '^$' -bench 'BenchmarkNeo4jReaderHealthyPolicyOverhead' -benchtime=200000x -count=5
go test ./internal/query -run '^$' -bench 'BenchmarkGraphStatementFingerprint' -benchtime=500000x -count=5
```

`BenchmarkNeo4jReaderHealthyPolicyOverhead` is the existing stubbed-driver
harness on the one-token statement `RETURN 1 AS value`. This branch's
`benchmarkUnboundedReaderRead` also calls `graphStatementFingerprint`, so the
control shape does the same per-read work as the production path; `origin/main`
runs its own unchanged harness. `BenchmarkGraphStatementFingerprint` is new: it
times the fingerprint alone on a 6-line, ~250-character statement with two inline
literals and a `$parameter`, the shape the scanner really walks. The previous
head has no such benchmark, so the same benchmark file was copied into its
checkout.

| Benchmark | `origin/main` / previous head median ns/op | This branch median ns/op | Before B/op | After B/op | Before allocs/op | After allocs/op |
| --- | --- | --- | --- | --- | --- | --- |
| Healthy overhead, `unbounded_reader` (vs `origin/main`) | 729.7 (range 531.0-1700) | 1229 (range 689.5-3892) | 1248 | 1512 | 19 | 23 |
| Healthy overhead, `bounded_reader` (vs `origin/main`) | 1470 (range 1019-2889) | 2071 (range 1018-4988) | 1544 | 1808 | 25 | 29 |
| `GraphStatementFingerprint`, realistic statement (vs previous head `4ea998f8a5`) | 1069 (range 837-1259) | 892.8 (range 672.6-1158) | 1056 | 672 | 5 | 4 |

Reading it honestly:

- The allocation columns are exact and identical across all ten samples per
  cell. Against `origin/main` the fingerprint adds +264 B/op and +4 allocs/op
  to a read on the stubbed harness. Against the previous head the redacting
  scanner allocates less on the realistic statement (672 B and 4 allocs, down
  from 1056 B and 5): the scanner writes into one pre-sized buffer instead of
  `strings.Fields` plus `strings.Join`. The four remaining allocations, from a
  `-memprofile` run with `-memprofilerate=1`, are the output buffer
  (`strings.Builder.Grow`), the `[]byte` copy handed to `sha256.Sum256`, and
  the two inside `hex.EncodeToString` (its byte buffer and the string).
- The ns/op columns are not a reliable signal on this host. This second run
  shared the machine with other agents' Go builds, which is why the ranges are
  wide (the first run of the same design, before the buffer headroom change, had
  a `bounded_reader` median of 1028 ns vs 1167 ns and a fingerprint median of
  696 ns vs 747 ns). Across both runs the healthy-path medians on the stubbed
  harness moved by well under 1 microsecond, and the fingerprint on a realistic
  statement stayed at or below the previous head's cost in the second run. The
  benchmark cannot resolve the difference from run-order and load noise, so this
  document claims the fixed allocation cost, not a nanosecond figure.
- Re-measured after the scanner learned Neo4j 5 digit separators, trailing
  number characters, and Unicode whitespace (review round 2): the scanner now
  decodes a rune for bytes at or above 0x80 and consumes trailing identifier
  characters after a number. A first version of that change cost about +125 ns
  per read on the realistic statement (median 746 vs 872 ns, interleaved runs of
  two compiled test binaries, 12 samples each); an ASCII lookup table in the
  identifier loop and testing identifier starts before numbers brought it to
  627 vs 655 ns (min 602 vs 643, +28 ns median), with identical allocations
  (672 B, 4 allocs) on both sides. Interleaving compiled binaries is what made
  the comparison usable on a shared host.
- No cache was added. Per-read cost is well under a microsecond even in the
  noisy run, and a cache keyed by statement text would put a shared structure on
  the read path to save less than the noise. Against the reader's 10-second
  deadline budget and any real Cypher execution, which dominates these shapes by
  orders of magnitude in production, the cost is negligible. No worker count,
  lease, batch size, or concurrency knob changed; this is a pure per-read
  constant-cost addition on the existing single-read path, not a new
  serialization point.

Focused proof: `cd go && env -u GOROOT go test ./internal/query -count=1` (all
`neo4j_read_policy*` tests, including the new fingerprint/threshold suite)
passes; see the PR's test evidence for the full RED/GREEN and mutation-testing
record.

## Observability Evidence

Observability Evidence:

- `neo4j.query` span gains `eshu.graph_read.statement_fingerprint`
  (`SpanAttrGraphReadStatementFingerprint`,
  `go/internal/telemetry/contract/graph_read.go`), set on every read
  regardless of outcome.
- `query.graph_read.warning` (slow/deadline/unavailable outcomes only) gains
  `graph_read.statement_fingerprint` and `graph_read.statement_head`
  (`LogKeyGraphReadStatementFingerprint`/`LogKeyGraphReadStatementHead`, same
  file). The head is the redacted, whitespace-collapsed statement shape
  (`go/internal/query/graph/statement`: every numeric and string
  literal replaced by `<REDACTED>`, booleans and null kept, comments dropped) truncated to 300 characters with an
  `...[truncated]` marker; neither field carries a bound parameter or an
  inline literal value.
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
