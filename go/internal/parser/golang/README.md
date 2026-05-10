# Go Parser

## Purpose

This package owns Go source parsing for the parent parser dispatcher. It turns
one Go source file into the payload buckets used by collector materialization,
and it provides the lighter pre-scan contracts that let same-package interface
evidence flow into later parse calls.

## Ownership boundary

This package is responsible for Go tree-sitter parsing, Go payload assembly,
Go dead-code root evidence, import alias tracking, receiver and call metadata,
function and method return-type metadata, composite-literal type references,
package interface pre-scan rows, and embedded SQL extraction.

The parent parser package still owns registry lookup, path normalization,
content metadata inference, runtime parser allocation, and the compatibility
methods on `Engine`. Shared payload and tree helpers come from
`internal/parser/shared`; this package must not import the parent parser
package.

## Exported surface

The godoc contract is in `doc.go`.

- `Parse` builds the full Go file payload.
- `PreScan` returns deterministic Go symbol names for import-map pre-scans.
- `ImportedInterfaceParamMethods` extracts imported-interface parameter
  contracts from one file for same-package dead-code evidence.
- `EmbeddedSQLQueries` returns typed SQL table evidence from recognized Go
  database call sites.
- `EmbeddedSQLQuery`, `Options`, and `GoImportedInterfaceParamMethods` carry the
  typed contracts used by those functions.

## Dependencies

This package imports `go/internal/parser/shared` for payload helpers,
tree-sitter node helpers, source reads, and parser options. It imports
`github.com/tree-sitter/go-tree-sitter` for the parser and node contracts.

It must not import collector, query, projector, reducer, storage, telemetry, or
the parent parser package.

## Telemetry

This package emits no metrics, spans, or logs. Parse timing and error
observation remain owned by the parent engine and collector runtime path.

## Gotchas / invariants

Payload bucket ordering is part of the fact-input contract. `Parse` sorts
functions, structs, interfaces, variables, imports, and function calls before
returning.

Function and method rows may carry `return_type` when tree-sitter exposes a
single named, pointer, selector, generic, or qualified result type. The value is
normalized to the terminal type name, so a pointer to an imported selector keeps
only the type name. Reducer code-call materialization uses that bounded evidence
for Go method chains.

Receiver inference is lexical, not whole-function. Constructor-assigned
variables use the nearest block, loop, switch case, or if statement as their
scope; typed parameters on declarations, methods, and function literals use the
function body. Range variables over locally known map values inherit the map
value type for calls such as `controllerDesc.BuildController`. A shadowed
variable in an inner block must not change calls that happen after that block.

Function-value reference rows are emitted only for identifiers in value
positions that are not locally bound at that source line. That includes call
arguments such as builder callbacks, composite literal fields, and returned
method values such as `runFuncSlice(rx).Run`. Package-level function literals
also mark same-file helper calls as `go.function_literal_reachable_call` when
the callee name is not shadowed inside the literal. This keeps callback wiring
visible across files while avoiding references for local variables that happen
to share a package-level function name.

Cyclomatic complexity counts Go control-flow branches once. The helper layer
counts a `for range` statement through the enclosing `for_statement`, not again
through its `range_clause`; this preserves the parent parser fixture contract.

Dead-code evidence is conservative. Handler signatures, Cobra run signatures,
controller-runtime reconciler signatures, registration calls, function-value
references, function-literal reachable calls, interface implementations, and
dependency-injection callbacks add `dead_code_root_kinds` only when the local
syntax or same-package pre-scan evidence proves the root.

`ImportedInterfaceParamMethods` is file-local by design. The parent `Engine`
groups those rows by package directory before passing them back through
`Options`.

Embedded SQL evidence only records recognized database/sql and sqlx call sites
where a string literal contains an obvious table reference. Line numbers refer
to the original Go source.

## Related docs

- `docs/plans/2026-05-09-parser-language-layout.md`
- `go/internal/parser/README.md`
- `go/internal/parser/shared/README.md`
