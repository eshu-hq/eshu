# Evidence — #3870 code-import DEPENDS_ON: field resolution + replay ordering

Two changes that together let a Go code-import resolve to a cross-repo
`DEPENDS_ON` edge end-to-end:

1. **Field resolution** (`go/internal/reducer/code_import_repo_edge.go`,
   `codeImportEntrySource`). The resolver read the import coordinate only from
   `resolved_source`/`source`, but the Go parser emits the import path under
   `name` with no `source` (`parser/golang/language.go` `import_spec`), so Go
   code-import edges never resolved. The fix prefers `source` and falls back to
   `name` only when `source` is empty (the Go case), keeping JS/TS/Python correct
   where `source` always carries the module specifier (`name` is the symbol).

2. **Replay ordering** (`go/internal/storage/postgres` +
   `go/cmd/bootstrap-index`). The code-import projection resolves owners through
   the cross-scope package-registry owner index, which is empty on the first pass
   (the ownership facts are produced in the same drain), so the projection
   succeeds as a retraction. `ReopenCodeImportRepoEdgeWorkItems` replays those
   succeeded work items once the ownership facts exist — mirroring the existing
   `ReopenDeploymentMappingWorkItems` reopen and run in the same maintenance
   transaction (`RunDeferredRelationshipMaintenance`, the ingester's
   post-shard-drain pass) and as a bootstrap-index post-collection phase.

## Performance

No-Regression Evidence: change 1 is a field-selection branch in the existing
per-import loop (one extra map lookup per entry; no new query/scan/fence). Change
2 reuses the existing reopen mechanism (a bounded `WHERE domain = '...' AND status
= 'succeeded'` listing + the existing `ReopenSucceeded` update) inside the reopen
transaction that already runs — it adds one indexed query and at most one update
per already-succeeded code-import work item, off the steady-state hot path
(post-drain maintenance only). Focused suites pass:
`go test ./internal/reducer ./internal/storage/postgres ./cmd/bootstrap-index`.
The B-7 golden-corpus gate over the corpus stays within budget
(`pipeline_wall_time` ~35s vs the 1800s ceiling, two drain passes) and now
produces the cross-repo edge via both `projection/code-imports` and
`projection/package-consumption`, with rc-3 required and green.

## Observability

No-Observability-Change: code-import outcomes already flow through the existing
`eshu_dp_code_import_repo_edges_total` counter (labels
`considered`/`written`/`skipped_*`); the field fix shifts Go imports from
`skipped_*` to `written` on that same signal, and the replay re-runs the same
domain whose counter already exists. The reopen logs a `code_import_repo_edge_reopened
count=N` line consistent with the existing `deployment_mapping_reopened` log. No
new metric, span, or log key is introduced.
