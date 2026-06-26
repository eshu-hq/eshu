# Evidence — #3870 code-import DEPENDS_ON field resolution

Scope: a correctness fix in `codeImportEntrySource`
(`go/internal/reducer/code_import_repo_edge.go`). The resolver read the import
coordinate only from `resolved_source`/`source`, but the Go parser emits the
import path under `name` with no `source` (`parser/golang/language.go`
`import_spec`), so Go code-import repo-to-repo `DEPENDS_ON` never resolved. The
fix prefers `source` and falls back to `name` only when `source` is empty (the Go
case), keeping JS/TS/Python correct where `source` always carries the module
specifier (and `name` is usually the imported symbol).

## Performance

No-Regression Evidence: the change is a field-selection branch inside the
existing per-import loop — one extra map lookup per import entry, no new query,
scan, fence, or allocation in any hot loop. The owner index and the rest of the
projection path are unchanged. The focused reducer suite
(`go test ./internal/reducer`) passes; new table tests assert Go name-only
imports resolve and that JS/Python keep resolving from the module `source` (never
the symbol `name`). The B-7 golden-corpus gate run over the corpus stays within
budget (`pipeline_wall_time` ~30s vs the 1800s ceiling) with rc-3 still green.

## Observability

No-Observability-Change: outcomes already flow through the existing
`eshu_dp_code_import_repo_edges_total` counter (labels
`considered`/`written`/`skipped_*`); fixing the field read shifts Go imports from
`skipped_*` to `written` on that same signal. No new metric, span, or log key is
introduced.
