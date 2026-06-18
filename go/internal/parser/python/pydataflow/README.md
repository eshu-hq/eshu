# Python CFG + Reaching Definitions

## Purpose

`pydataflow` lowers a Python function into a control-flow graph and resolves
reaching definitions over it, reusing the language-neutral `internal/parser/cfg`
engine. It is the Python counterpart of the Go and TS/JS lowerings and a step
toward Python value-flow taint (epic #2705, issue #2826).

## Ownership boundary

This package owns the Python tree-sitter-to-CFG lowering and binding extraction.
It does NOT own the dataflow algorithm (`internal/parser/cfg`), taint semantics,
source/sink catalogs, or summary composition — those are language neutral and
shared. It does not emit parser payload buckets directly; the `python` adapter
(`cfg_emit.go`) drives this lowering and renders the `dataflow_functions`,
`taint_findings`, and `interproc_findings` buckets behind `Options.EmitDataflow`.

## Exported surface

See `doc.go` for the godoc contract. The surface is:

- `LowerFunction(node, source, limits) cfg.Function` — lower one Python function
  definition body into a resolved control-flow graph.
- `TaintFacts(node, source, fn) taint.Facts` — derive intraprocedural taint
  annotations (sources, sinks, sanitizers) from the Python catalog, mapped onto
  the control-flow graph, for the `internal/parser/taint` engine. Sources require
  qualified framework request annotations or import-backed request aliases; sinks
  require a qualified receiver/module except for Python builtins.
- `TaintCatalogVersion() string` — deterministic SHA-256 content hash for the
  Python taint catalog, emitted by the parser so collector freshness changes
  when catalog-only matching rules change.
- `EffectsSpec(node, source, fn, localFuncs) valueflow.EffectsSpec`,
  `LocalFunctionIDs`, `FunctionID` — build a function's value-flow summary spec
  (params, sources/sinks/sanitizers, returns, intra-file call-arg sites) for
  cross-function composition.
- `InterprocFindings(root, source, repositoryID, importPath) []interproc.Finding`
  — compose the per-function summaries of a file into an interprocedural port
  graph and solve it, returning the cross-function taint findings. Resolution is
  intra-file; `repositoryID` must be stable and generation-independent for
  durable summary persistence.

## Dependencies

- `internal/parser/cfg` (the dataflow engine), `internal/parser/taint` (the
  taint fact types), `internal/parser/valueflow` + `internal/parser/interproc` +
  `internal/parser/summary` (cross-function composition), `internal/parser/shared`
  (node text/line helpers), and `github.com/tree-sitter/go-tree-sitter`.

## Telemetry

None. The lowering is a pure function; a reducer that drives the pipeline owns
telemetry.

No-Regression Evidence: import-aware request source matching stays inside the
pure per-file taint catalog path and does not change CFG lowering, graph writes,
queue behavior, or parser dispatch. Verified by
`go test ./internal/parser/python ./internal/parser/python/pydataflow ./internal/parser -count=1`
and the Go no-regression focused gate
`go test ./internal/parser/golang ./internal/parser -run 'TestGo.*Taint|TestGo.*Dataflow|Test.*Catalog' -count=1`.

No-Observability-Change: the matcher adds no metric, span, log, status field,
runtime knob, queue, worker, or graph query. Operators still diagnose parser
cost through existing collector parse-stage logs and
`eshu_dp_file_parse_duration_seconds`.

## Performance and concurrency

`InterprocFindings` composes per-function summaries and calls
`interproc.SolvePartitioned`. It introduces no new goroutines, channels, locks,
or shared state of its own — the partitioned, race-free fixpoint lives in
`internal/parser/interproc` and is proven there.

- No-Regression Evidence: the cross-function pass is bounded by the cfg and
  interproc limits and runs per file off the parse tree; it adds no graph write,
  Cypher, queue, or worker behavior. Measurement is the package test suite
  (`go test ./internal/parser/python/pydataflow -count=1`), which exercises the
  interprocedural composition on small fixtures deterministically.
- No-Observability-Change: pure functions with no telemetry surface; an operator
  observes this work through the reducer that drives the pipeline, unchanged here.

## Gotchas / invariants

- **`assignment` lives inside an `expression_statement`** and carries `left`/
  `right` fields; `augmented_assignment` (`+=`) reads and writes its target.
- **`for_statement` is for-in**: `left` is the loop target (defined each
  iteration), `right` is the iterable (read). A back-edge wires the body to the
  header.
- **`if_statement`/`elif_clause`** carry `condition`/`consequence`/`alternative`;
  `else_clause` and `elif_clause` alternatives are descended into.
- **`with_statement`** binds `as` aliases (defs) and reads context managers
  (uses) on the header line, then lowers the body in sequence — so a sink call
  inside `with conn.cursor() as cursor:` gets its own statement line and resolves.
- **`try_statement`** lowers the body from the current block; each
  except/else/finally handler branches from the **pre-try** state. This records
  the handlers' inner statements without inventing a body-completed definition
  that reaches a handler (which would be a false edge); the under-modeled
  body→handler flow is a safe false negative.
- **Attribute access `a.b`**: only `a` (the object) is a use; `b` is the
  attribute name in the grammar (an `identifier`), so `exprUses` skips it to
  avoid a false use of a same-named variable.
- **Tuple/list targets** (`a, b = ...`) define each identifier; attribute and
  subscript targets read their base, never define.
- Nested function/lambda bodies are not descended into (a safe false negative,
  never a false edge).
- **Taint catalog (`taintfacts.go`) is conservative.** Sources require typed
  framework request parameters with qualified annotations or known framework
  imports such as FastAPI, Flask, Starlette, or Django. A local type named
  `Request` is not enough. Sinks require known receivers or modules such as
  `cursor.execute` or `os.system`, except for Python builtins such as `eval` and
  `exec`. Sanitizers are narrow and unambiguous
  (`escape` → html); a name that neutralizes different kinds by import (`quote`
  is urllib URL-encoding but also shlex shell-quoting) is omitted, since a missed
  sanitizer is safer than a missed vulnerability.
- **A sanitizer is recorded only when the assigned value is DIRECTLY a sanitizer
  call.** `safe = escape(x) if cond else x` is not marked sanitized — the other
  branch is unneutralized, so marking it would wrongly suppress a real finding.
- **Interprocedural resolution is intra-file and conservative.** Only top-level
  `function_definition`s are entries (`LocalFunctionIDs`/`InterprocFindings` do
  not descend into nested function bodies — closures are a later pass), and only
  bare-identifier calls resolve to a local callee. A method call (`cursor.execute`)
  or a nested private function never invents a false cross-function edge. A call
  whose name is shadowed by a local binding at or before the call is also skipped.

## Related docs

- Epic #2705, issue #2826 (TS/JS + Python lowering). Mirrors the Go lowering in
  `internal/parser/golang` and the TS/JS lowering in
  `internal/parser/javascript/jsdataflow`.
