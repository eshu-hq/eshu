# Go CFG + Reaching Definitions

## Purpose

`dataflow` lowers a Go function, method, or function literal body into a
control-flow graph and resolves reaching definitions over it, reusing the
language-neutral `internal/parser/cfg` engine. It is the Go counterpart of
`internal/parser/javascript/jsdataflow`, extracted from `internal/parser/golang`
(issue #6774) so the root `golang` package stops importing everything it needs
from one flat, cycle-prone file set.

## Ownership boundary

This package owns the Go tree-sitter-to-CFG lowering, binding/access-path
extraction, guard-text rendering, and the intraprocedural taint catalog for Go.
It does NOT own the dataflow algorithm (that is `internal/parser/cfg`), taint
semantics or the solver (`internal/parser/taint`, `internal/parser/interproc`),
or symbol/parent-lookup/receiver-context helpers shared across the `golang`
parser (those live in `internal/parser/golang/symbols`). It never imports
`internal/parser/golang` — that back-edge is exactly the cycle #6774 removes.

## Exported surface

See `doc.go` for the full godoc contract. The surface is deliberately small:

- `EmitBuckets(root, source) (functions, findings []map[string]any)` — lower
  every top-level function/method once and render the `dataflow_functions` and
  `taint_findings` payload rows.
- `InterprocPayloads(root, source, repositoryID, importPath) (findings,
  summaries, sourceRows []map[string]any)` — derive a value-flow summary per
  function, compose an interprocedural port graph, and render
  `interproc_findings`, `dataflow_summaries`, and `dataflow_sources`.

Both are called directly from `internal/parser/golang/language.go`'s `Parse`,
behind `Options.EmitDataflow`. Everything else (the CFG lowerer, binding
extraction, access-path resolution, guard-text redaction, and the taint
catalog) is internal: no other package needs it, so it stays unexported.

## Dependencies

- `internal/parser/cfg` (the dataflow engine), `internal/parser/taint`,
  `internal/parser/summary`, `internal/parser/valueflow`,
  `internal/parser/interproc` (the value-flow engines), `internal/parser/shared`
  (node text/line helpers), `internal/parser/dataflowemit` (row rendering),
  `internal/parser/golang/symbols` (receiver context, scope walking, first
  named descendant), and `github.com/tree-sitter/go-tree-sitter`.

## Telemetry

None. The lowering is a pure function; a reducer that drives the ingest
pipeline owns telemetry for the stage that calls it.

No-Regression Evidence: this is a pure package-move (issue #6774 stage 2,
`golang/cfg_*.go` -> `golang/dataflow/*.go`, `package golang` -> `package
dataflow`). No function body changed beyond swapping the root's one-line
`nodeText`/`nodeLine`/`walkNamed` forwarders for the `internal/parser/shared`
calls they forwarded to, and renaming the two symbols the root still calls
(`goEmitDataflowBuckets` -> `EmitBuckets`, `goInterprocPayloads` ->
`InterprocPayloads`) to their new exported names. Verified by
`go test ./internal/parser/golang/... -count=1` and the accuracy golden gate
(`scripts/verify_accuracy_golden_gate.sh`), both green on the moved code.

No-Observability-Change: the move adds no metric, span, log, status field,
runtime knob, queue, worker, or graph query.

## Gotchas / invariants

- **`block` wraps a `statement_list`** in the Go tree-sitter grammar;
  `lowerStmt` recurses through either node kind to reach the real statements.
- **Access paths are field-sensitive** (`access_paths.go`): a selector target
  defines its full path, a subscript collapses to the container's `[*]`
  approximation, and only a direct `&x` pointer alias (and simple alias
  chains) normalize a field write back to the original struct — a plain value
  copy (`copy := data`) is never treated as an alias.
- **Closures are only descended into as call arguments**
  (`goFuncLiteralCaptureUses` in `bindings.go`): a function literal passed to
  a call contributes its captured (free) variables to the enclosing function;
  a literal that is merely assigned or returned is not descended into.
- **Guard text is redacted and whitespace-normalized** (`guard_text.go`):
  string/rune/numeric literals become `<literal>` before the predicate is
  stored, so control-dependence provenance never carries a source secret.
  `goNegatedGuardText` special-cases a whole top-level `!(...)` operand so a
  double negation round-trips instead of accumulating `!(!(...))`.
- **The taint catalog is intentionally small** (`taint_facts.go`): source,
  sink, and sanitizer names are matched conservatively (qualified base.field
  for template/exec sinks, bare method name for database/sql calls) to avoid
  a same-named method on an unrelated type registering a false finding.
- **`goLineIndex` matches by source line**, which is exact for idiomatic
  one-statement-per-line Go; two calls sharing a line are a known intra-file
  precision limit (a safe false negative, never a false edge).

## Related docs

- Issue #6774 (parser/golang leaf split); the Go template this package was
  extracted from is documented by its former home,
  `internal/parser/golang/README.md`.
- `internal/parser/javascript/jsdataflow/README.md` — the TS/JS sibling this
  package's design (and this package's file layout) mirrors.
