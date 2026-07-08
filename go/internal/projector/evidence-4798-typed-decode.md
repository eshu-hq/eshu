# Projector Typed Decode Evidence (#4798)

This note records the local proof for the #4798 projector typed-decode slice.
The change moves codegraph repository/file canonical extraction, repository
runtime helpers, and the single-field AWS/IAM/code-dataflow reducer-intent
admission reads behind factschema decoders.

Performance Evidence: the changed projector path is still a pure in-memory
projection build before any graph backend write. The benchmark input is one
repository fact plus 1,000 `codegraph.file` facts, all schema `1.0.0`, producing
1,000 `FileRow` rows and zero quarantined facts. Baseline was `origin/main`
`a6c65635a26ec68db90967d296ebaac5042d4b16` with the same benchmark shim:
379-444 us/op, about 1,574,4xx B/op, and 1,075 allocs/op. After the typed decode
change and specialized `DecodeCodegraphFile` path, local Go 1.26.4
darwin/arm64 on Apple M5 Max measured 424-448 us/op, 1,590,663-1,590,666 B/op,
and 2,080 allocs/op. The path remains sub-millisecond for 1,000 file facts; the
extra allocation is bounded to optional pointer fields and the graph writer,
queue claim, worker count, and backend transaction shape are unchanged.

Benchmark Evidence:

```text
cd go
go test ./internal/projector -run '^$' -bench BenchmarkBuildCanonicalMaterializationCodegraphFiles -benchmem -count=5
BenchmarkBuildCanonicalMaterializationCodegraphFiles-18  2584  447917 ns/op  1590666 B/op  2080 allocs/op
BenchmarkBuildCanonicalMaterializationCodegraphFiles-18  2844  431420 ns/op  1590664 B/op  2080 allocs/op
BenchmarkBuildCanonicalMaterializationCodegraphFiles-18  2845  428583 ns/op  1590664 B/op  2080 allocs/op
BenchmarkBuildCanonicalMaterializationCodegraphFiles-18  2862  424700 ns/op  1590664 B/op  2080 allocs/op
BenchmarkBuildCanonicalMaterializationCodegraphFiles-18  2494  433328 ns/op  1590663 B/op  2080 allocs/op
PASS
```

No-Regression Evidence: `GOCACHE=/tmp/eshu-4798-gocache make pre-pr` passed
after the telemetry coverage fix. The local gate covered gofumpt, whole-module
golangci-lint, `go build ./...`, `go vet ./...`, changed-package
`go test ./internal/projector -count=1`, 500-line file cap, package docs,
payload usage manifest, capability inventory docs, telemetry coverage,
operator dashboard generation, code coverage report, and the race graph-writes
lane for `./internal/storage/cypher/... ./internal/reducer/...
./internal/projector/... ./internal/correlation/... ./internal/content/shape/...
./internal/relationships/...`. Focused gates also passed:
`go test ./internal/projector -count=1`, `go test ./... -count=1` under
`sdk/go/factschema`, `go test ./internal/goldengate/...
./cmd/golden-corpus-gate/ -count=1`, `bash
scripts/test-verify-golden-corpus-gate.sh`, and
`scripts/verify-parser-relationship-kit.sh`.

Observability Evidence: malformed required codegraph repository/file fields are
partitioned as `input_invalid` projector decode failures and recorded through
the existing `eshu_dp_projector_input_invalid_facts_total` counter plus the
structured `projector input_invalid fact quarantined` log, labeled with
`stage=codegraph_canonical` and the fact kind. `docs/public/observability/
telemetry-coverage.md` now maps the new projector typed-decode files to that
counter or to explicit no-observability-change rows, and
`ESHU_TELEMETRY_COVERAGE_BASE=origin/main bash
scripts/verify-telemetry-coverage.sh` passed.

No-Observability-Change: AWS/IAM/code-dataflow single-field intent decoders
only gate reducer-intent admission and skip invalid candidates the same way the
previous raw field reads skipped absent or unusable fields. They do not add
queue rows, graph writes, retries, leases, or backend calls, so the existing
projector run/stage duration metrics and reducer-intent enqueue metrics remain
the operator-facing signals for admission volume and latency.
