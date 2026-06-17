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
neutral and shared. It does not emit parser payload buckets; wiring the lowering
into the `javascript` adapter's payload is a later step.

## Exported surface

See `doc.go` for the godoc contract. The surface is:

- `LowerFunction(node, source, limits) cfg.Function` — lower one TS/JS function,
  method, or arrow-function body into a resolved control-flow graph.

## Dependencies

- `internal/parser/cfg` (the dataflow engine), `internal/parser/shared` (node
  text/line helpers), and `github.com/tree-sitter/go-tree-sitter`.

## Telemetry

None. The lowering is a pure function; a reducer that drives the pipeline owns
telemetry.

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
- **Bounded + deterministic** via the cfg engine; counted overflow, never a
  silent drop.

## Related docs

- Epic #2705, issue #2826 (TS/JS + Python lowering). Mirrors the Go lowering in
  `internal/parser/golang` (`cfg_lower.go`, `cfg_bindings.go`).
