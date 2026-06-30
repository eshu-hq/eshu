# Evidence — code_call_materialization file split (#3788)

Epic D / D-2 modularization. This note records the performance and observability
evidence for splitting four oversized `code_call_materialization` source files
into same-package sibling files. It exists so the hot-path evidence gate
(`scripts/verify-performance-evidence.sh`) has a tracked, in-repo record rather
than relying on PR prose.

## Change shape

Pure same-package file split — no logic was edited, only moved:

- `code_call_materialization.go` (488 → 349) → `_extract.go` (row-extraction entrypoints)
- `code_call_materialization_helpers.go` (498 → 343) → `_path_helpers.go`
- `code_call_materialization_imports.go` (489 → 234) → `_imports_resolve.go`
- `code_call_materialization_index.go` (501 → 294) → `_index_rows.go`

The Go compiler treats files in a package as one translation unit, so file
boundaries do not affect emitted code. Per-file imports were narrowed to what
each split file uses.

## No-Regression Evidence:

- **Baseline / after:** the code-call materialization domain's logic is byte-for-byte
  unchanged — every function body was moved verbatim. Structural diff confirms all
  66 top-level declarations (func/type/const/var) from the four originals are
  present exactly once across the eight resulting files; no statement was edited.
- **Backend/version:** no graph backend interaction changed; the emitted Cypher
  edge-write shape (`code_call` caller→callee rows) and the deterministic row sort
  in `extractCodeCallRowsWithIndex` are unchanged (moved, not modified).
- **Input shape / terminal counts:** the row set produced by `ExtractCodeCallRows`
  / `ExtractAllCodeRelationshipRows` for a given envelope set is identical — same
  builders, same ordering, same dedup keys. No queue, batch, worker, or row-count
  behavior changed.
- **Why safe:** the full reducer package test suite (`go test ./internal/reducer/
  -count=1`, including the ~80 `code_call_materialization_*_test.go` suites) is
  green before and after the split; `go vet` and `golangci-lint run` (filelength
  plugin loaded) report 0 issues. A pure code-movement refactor cannot regress
  throughput or correctness because no instruction changed.

## No-Observability-Change:

No spans, metrics, logs, or telemetry names were added, removed, or renamed. The
code-call materialization domain remains covered by `eshu_dp_reducer_executions_total`,
`eshu_dp_reducer_run_duration_seconds`, `eshu_dp_code_call_edge_batches_total`, and
`eshu_dp_code_call_edge_batch_duration_seconds`; the four new files are pure
extraction helpers that emit no metric of their own (recorded in the X1 telemetry
contract `docs/public/observability/telemetry-coverage.md`).
