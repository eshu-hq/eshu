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

No-Regression Evidence: the field-sensitive access-path, container-element,
reference-alias, and closure-capture lowering (#3252/#3253) is a pure
per-function CPU change that adds binding precision; it touches no graph write,
queue, lease, worker, or parser dispatch. It is strictly more precise (member
targets now define access paths instead of dropping the def; member reads still
record the base object, so whole-object flow is preserved), so it can add a
reaching-def edge but never invents a false edge, and path truncation is counted
in `Overflow.AccessPaths` rather than dropped. The earlier import-aware request
source matcher likewise stays inside the pure per-file taint catalog path.
Verified by
`go test ./internal/parser/javascript ./internal/parser/javascript/jsdataflow ./internal/parser/... -count=1`
(full parser tree green, including the `dataflowemit`, `valueflow`, `interproc`,
and `taint` consumers) and the Go no-regression focused gate
`go test ./internal/parser/golang ./internal/parser -run 'TestGo.*Taint|TestGo.*Dataflow|Test.*Catalog' -count=1`.

No-Observability-Change: the lowering adds no metric, span, log, status field,
runtime knob, queue, worker, or graph query. Operators still diagnose parser
cost through existing collector parse-stage logs and
`eshu_dp_file_parse_duration_seconds`.

## Field-sensitive precision (`accesspaths.go`)

Mirrors the Go template (`internal/parser/golang/cfg_access_paths.go`, issues
#2999/#3000) so taint that flows through a field, container element, or reference
alias is no longer a whole-binding false negative (#3252):

- **Field-sensitive access paths**: a member target `obj.field = x` defines the
  binding `obj.field`; a member read `obj.field` uses `obj.field` plus the base
  object `obj`. A path deeper than `cfg.Limits.MaxAccessPathParts` truncates to
  its prefix plus a `*` segment and counts `Overflow.AccessPaths`, so the write
  and read of the same deep path still match and truncation is never silent.
- **Container-element flow**: a subscript `m[k]` / `arr[i]` lowers to the
  explicitly labeled whole-container approximation `m[*]` / `arr[*]` (over the
  whole container, never a fabricated per-key precision).
- **Reference aliases**: a plain `let a = obj` makes `a` an alias of `obj` (JS
  has no address-of operator), so a field write `a.field = x` normalizes to
  `obj.field`. Only the base segment of a multi-part access path is alias-resolved;
  bare identifier reads keep their own reaching-def identity, so simple value flow
  is unchanged. The alias map is cloned per if/else branch, merged by
  intersection, and intersected with the body-exit alias state after loops — an
  alias on one path or changed by a possible loop iteration never leaks.

## Closure/captured-variable flow (`jsFuncLiteralCaptureUses` in `bindings.go`)

A function literal/arrow passed as a call argument (`doThing(() => sink(v))`,
`arr.forEach(x => use(v))`) is descended into for the enclosing function's uses,
attributing the closure's free variables (its body uses minus its own parameters
and inner-scope `let`/`const`/`var` definitions, so inner-scope shadowing holds).
A function literal that is **not** invoked is still not descended into.

## Gotchas / invariants

- **`statement_block` holds statements directly** in the TS/JS grammar (unlike
  Go, where a block wraps a `statement_list`).
- **`lexical_declaration` → `variable_declarator`** with `name`/`value` fields;
  one CFG statement is emitted per declarator.
- **`augmented_assignment_expression` (`+=`) and `update_expression` (`x++`)**
  both read and write their target; a plain `assignment_expression` only writes.
  A member/subscript target is a field-sensitive access-path definition.
- **Nested function/arrow bodies are descended into only when the literal is a
  call argument** (closure capture); a non-invoked literal is not descended into,
  a safe false negative, never a false edge.
- **Request source evidence is import-aware**: unqualified request type aliases
  must come from known framework modules such as Express, Fastify, Next.js, or
  Koa. A local type named `Request` is not enough.
- **Bounded + deterministic** via the cfg engine; counted overflow, never a
  silent drop.

## Related docs

- Epic #2705, issue #2826 (TS/JS + Python lowering). Mirrors the Go lowering in
  `internal/parser/golang` (`cfg_lower.go`, `cfg_bindings.go`).
