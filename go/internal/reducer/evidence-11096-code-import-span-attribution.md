# Evidence — #11096 incremental code-import load attribution

`CodeImportRepoEdgeHandler.Handle` (`go/internal/reducer/code_import_repo_edge_handler.go`)
loads scope-local `file` facts first and cross-scope package-ownership facts
second (#11082). Both span attributes and both load durations were emitted only
after the second load succeeded, so a hang between the two loads left the
completed file-load cost unattributed on the span. The fix records each step's
span attribute as soon as that step finishes: `file_load_ms` right after the
file load, then `ownership_load_ms` + `ownership_rows` after the ownership
load. Load behavior, error paths, verdicts, the completion-log call, and
metric families are untouched.

## Performance

No-Regression Evidence (#11096): the change splits one batched
`span.SetAttributes` call into two calls with the same three attribute keys
and the same value sources; no query, scan, fence, branch, or allocation is
added to either load path, and the nil-span guard is preserved on both calls.
RED proof: the new hang test failed pre-fix in 5.00s (`span is missing
"eshu.reducer.code_import_repo_edge.file_load_ms" while the ownership load
hangs`). After the fix, `go test ./internal/reducer/ -run
'TestCodeImportRepoEdge' -count=1` passes (~0.02s), the full
`go test ./internal/reducer/ -count=1` package suite passes (7.389s), and
`go vet ./internal/reducer/` is clean. Backend: unit-level proof with the
in-memory OTEL SDK tracer (go.mod pins `go.opentelemetry.io/otel/sdk v1.47.0`)
and stub loaders — no live Neo4j/Postgres involved. Input shape: one file
envelope served immediately, ownership load blocked on `ctx.Done()`. Terminal
state: the handler stays blocked (no premature return, no intents written)
until cancel, then returns the wrapped ownership-load error; all pre-existing
written/refreshed intent counts are unchanged (suite green).

## Observability

Observability Evidence (#11096): while the ownership load hangs, the live
handler span now carries `eshu.reducer.code_import_repo_edge.file_load_ms`
(polled from the un-ended SDK span in
`TestCodeImportRepoEdgeHandlerAttributesFileLoadBeforeOwnershipHang`), so the
next ownership-load hang names the completed step without pg archaeology. No
attribute key is added or renamed; `ownership_load_ms` and `ownership_rows`
still land on the same span after the ownership load. The completion log keeps
its single `logCompleted` call with the same `file_load`/`ownership_load`/
`ownership_rows` keys, and no metric instrument, label, or status field
changes.
