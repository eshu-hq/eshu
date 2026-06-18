# TypeScript/JavaScript CFG + Reaching Definitions

## Purpose

`jsdataflow` lowers a TypeScript or JavaScript function into a control-flow graph
and resolves reaching definitions over it, reusing the language-neutral
`internal/parser/cfg` engine. It is the TS/JS counterpart of the Go lowering and
the first step toward TS/JS value-flow taint (epic #2705, issue #2826).

## Ownership boundary

This package owns the TS/JS tree-sitter-to-CFG lowering and binding extraction.
It does NOT own the dataflow algorithm (that is `internal/parser/cfg`), taint
semantics, source/sink catalogs, or summary composition — those are language
neutral and shared. It does not emit parser payload buckets directly; the
`javascript` adapter (`cfg_emit.go`) drives this lowering and renders the
`dataflow_functions`, `taint_findings`, and `interproc_findings` buckets behind
`Options.EmitDataflow`.

## Exported surface

See `doc.go` for the godoc contract. The surface is:

- `LowerFunction(node, source, limits) cfg.Function` — lower one TS/JS function,
  method, or arrow-function body into a resolved control-flow graph.
- `TaintFacts(node, source, fn) taint.Facts` — derive intraprocedural taint
  annotations (sources, sinks, sanitizers) from the TS/JS catalog, mapped onto
  the control-flow graph, for the `internal/parser/taint` engine. Sources require
  qualified framework request annotations or import-backed request aliases; sinks
  require a qualified receiver/module except for language builtins.
- `TaintCatalogVersion() string` — deterministic SHA-256 content hash for the
  TS/JS taint catalog, emitted by the parser so collector freshness changes when
  catalog-only matching rules change.
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

- `internal/parser/cfg` (the dataflow engine), `internal/parser/taint`,
  `internal/parser/summary`, `internal/parser/valueflow`, `internal/parser/interproc`
  (the value-flow engines), `internal/parser/shared` (node helpers), and
  `github.com/tree-sitter/go-tree-sitter`.

## Telemetry

None. The lowering is a pure function; a reducer that drives the pipeline owns
telemetry.

No-Regression Evidence: import-aware request source matching stays inside the
pure per-file taint catalog path and does not change CFG lowering, graph writes,
queue behavior, or parser dispatch. Verified by
`go test ./internal/parser/javascript ./internal/parser/javascript/jsdataflow ./internal/parser -count=1`
and the Go no-regression focused gate
`go test ./internal/parser/golang ./internal/parser -run 'TestGo.*Taint|TestGo.*Dataflow|Test.*Catalog' -count=1`.

No-Observability-Change: the matcher adds no metric, span, log, status field,
runtime knob, queue, worker, or graph query. Operators still diagnose parser
cost through existing collector parse-stage logs and
`eshu_dp_file_parse_duration_seconds`.

## Gotchas / invariants

- **`statement_block` holds statements directly** in the TS/JS grammar (unlike
  Go, where a block wraps a `statement_list`).
- **`lexical_declaration` → `variable_declarator`** with `name`/`value` fields;
  one CFG statement is emitted per declarator.
- **`augmented_assignment_expression` (`+=`) and `update_expression` (`x++`)**
  both read and write their target; a plain `assignment_expression` only writes.
- **Nested function/arrow bodies are not descended into** for the enclosing
  function's uses, so a closure's captures are not attributed here (closures are
  a later pass) — a safe false negative, never a false edge.
- **Request source evidence is import-aware**: unqualified request type aliases
  must come from known framework modules such as Express, Fastify, Next.js, or
  Koa. A local type named `Request` is not enough.
- **Bounded + deterministic** via the cfg engine; counted overflow, never a
  silent drop.

## Related docs

- Epic #2705, issue #2826 (TS/JS + Python lowering). Mirrors the Go lowering in
  `internal/parser/golang` (`cfg_lower.go`, `cfg_bindings.go`).
