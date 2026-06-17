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
shared. It does not emit parser payload buckets; wiring into the `python`
adapter's payload is a later step.

## Exported surface

See `doc.go` for the godoc contract. The surface is:

- `LowerFunction(node, source, limits) cfg.Function` — lower one Python function
  definition body into a resolved control-flow graph.

## Dependencies

- `internal/parser/cfg` (the dataflow engine), `internal/parser/shared` (node
  text/line helpers), and `github.com/tree-sitter/go-tree-sitter`.

## Telemetry

None. The lowering is a pure function; a reducer that drives the pipeline owns
telemetry.

## Gotchas / invariants

- **`assignment` lives inside an `expression_statement`** and carries `left`/
  `right` fields; `augmented_assignment` (`+=`) reads and writes its target.
- **`for_statement` is for-in**: `left` is the loop target (defined each
  iteration), `right` is the iterable (read). A back-edge wires the body to the
  header.
- **`if_statement`/`elif_clause`** carry `condition`/`consequence`/`alternative`;
  `else_clause` and `elif_clause` alternatives are descended into.
- **Attribute access `a.b`**: only `a` (the object) is a use; `b` is the
  attribute name in the grammar (an `identifier`), so `exprUses` skips it to
  avoid a false use of a same-named variable.
- **Tuple/list targets** (`a, b = ...`) define each identifier; attribute and
  subscript targets read their base, never define.
- Nested function/lambda bodies are not descended into (a safe false negative,
  never a false edge).

## Related docs

- Epic #2705, issue #2826 (TS/JS + Python lowering). Mirrors the Go lowering in
  `internal/parser/golang` and the TS/JS lowering in
  `internal/parser/javascript/jsdataflow`.
