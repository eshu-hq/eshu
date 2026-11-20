# #10028 R1 scan provenance: no-regression note

Refs #10028, refs #10002. The R1 change threads four optional govulncheck
scan-provenance fields (`scan_level`, `scan_mode`, `scanner_version`,
`trace_count`) from the typed `GoCallReachability` decode through the
extract row, the classified finding, the impact-finding stash, and
`SupplyChainReachability` into the explain payload, emitting each key only
when the fact reports it. No Cypher, graph write, queue, lease, worker,
batching, migration, or concurrency change; the level→reachability switch
is untouched.

## Why it is safe

The change copies four scalar fields per winning (advisory, module) row
along the existing single-owner chain; no new allocation shape, no new
branch in a hot loop, no new I/O or lock. Pre-provenance and non-Go
findings keep byte-identical payloads by construction (opt-in keys) and by
pinned test (`TestGoScanProvenanceAbsentStaysAbsent` asserts all four keys
absent). Import→reachable mapping is unchanged
(`TestEmitterRulesNeverEmitImportForSymbolLevelScan` green); stdlib silence
is unchanged.

No-Regression Evidence: `go test ./internal/reducer/supplychain/core/
-count=1` → ok 0.756s, 1121 subtests pass on the final head; the same
suite passed on the base with the pre-existing tests only, and the three
new provenance tests fail to build without the fix (field absence) and
pass with it. `go test -race` on the changed package passes via the
pre-push floor; `go vet` clean; `gofmt -l` empty; `git diff --check`
clean; `bash scripts/verify-factschema-diff.sh` exit 0 (no fact-schema
change — the change only reads existing optional fields).
Backend/version: hermetic Go suites, Go toolchain in repo, no live
backend touched; input shape: in-memory fixtures plus the recorded
govulncheck streams, e.g.
`go/internal/reducer/supplychain/core/testdata/govulncheck/xtext_v0_3_5_symbol_source.jsonl`.

No-Observability-Change: the change adds no metric, span, log key, queue,
reducer lane, graph write, route, or runtime knob. Operators see the new
payload keys on Go rows in the existing explain surface; diagnosis flows
through the same reducer suite and explain handler as before.
