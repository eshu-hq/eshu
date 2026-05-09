# AGENTS.md - internal/parser/golang guidance

## Read first

1. `README.md` - package boundary, exported surface, and invariants
2. `doc.go` - godoc contract for the Go adapter package
3. `language.go` - `Parse`, `PreScan`, payload assembly, call metadata, and
   receiver handling
4. `dead_code_roots.go` - signature roots, import aliases, and root-kind
   helpers
5. `dead_code_registrations.go` - net/http and Cobra registration evidence
6. `dead_code_semantic_roots.go` - top-level semantic root collection
7. `dead_code_semantic_helpers.go` and `dead_code_semantic_flows.go` -
   interface, callback, field, and argument flow helpers
8. `package_interface_prescan.go` - imported-interface parameter extraction
9. `embedded_sql.go` - SQL literal extraction and line-number accounting
10. `helpers.go` and `types.go` - local helper and shared contract aliases
11. Parent tests in `go/internal/parser/go*_test.go` before changing emitted
    payload shape

## Invariants this package enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import `internal/parser`.
- `Parse` returns the same bucket names and map fields the parent Go adapter
  returned before the language-owned move.
- Bucket ordering is deterministic. Sort output before returning any payload or
  pre-scan result.
- Dead-code roots must be evidence-backed. Do not add root kinds from name
  guesses without syntax, registration, same-file, or same-package proof.
- `ImportedInterfaceParamMethods` stays file-local. Package grouping belongs in
  the parent `Engine` wrapper.
- Embedded SQL line numbers must refer to the original Go source, not a sliced
  function body.

## Common changes and how to scope them

- Add a new Go payload field by writing or updating a parent Go parser test
  first, then changing `language.go` or the focused helper that owns the
  evidence.
- Add a new dead-code root by adding a focused parent dead-code test first,
  then editing the narrow helper that owns that evidence family.
- Add SQL API support by writing a focused `embedded_sql_test.go` case first.
- Change same-package interface evidence by testing both
  `ImportedInterfaceParamMethods` and the parent package pre-scan wrapper.
- Keep compatibility wrappers in the parent parser package thin. New Go
  parsing behavior belongs here unless it is registry, runtime, or content
  metadata wiring.

## Failure modes and how to debug

- Missing Go functions, structs, interfaces, imports, variables, or calls
  usually means `language.go` skipped a tree-sitter node kind or changed bucket
  names. Compare the focused parent `go*_test.go` fixture output.
- Missing interface implementation roots usually means
  `ImportedInterfaceParamMethods` was not passed back through `Options`, or a
  type-flow helper lost local concrete-type evidence.
- Missing handler or Cobra roots usually means import alias evidence changed in
  `dead_code_roots.go` or registration evidence changed in
  `dead_code_registrations.go`.
- Wrong embedded SQL rows usually mean the call-site matcher or SQL table
  matcher in `embedded_sql.go` became too broad or too narrow.
- Non-deterministic import-map output usually means a helper returned unsorted
  names or iterated a map without sorting.

## Anti-patterns specific to this package

- Importing the parent parser package to reuse `Engine`, `Options`, payload, or
  tree helpers.
- Adding JavaScript, Python, Java, SQL, YAML, JSON, or other language behavior
  here.
- Returning partial payloads after a Go parse failure.
- Treating every composite literal, function value, or selector expression as
  live code without bounded evidence.
- Adding telemetry from this package; parse timing belongs to the runtime path
  that invokes the adapter.

## What NOT to change without an ADR

- Do not change parent registry extension ownership for `.go` files.
- Do not change the `embedded_sql_queries`, `dead_code_root_kinds`, or
  `function_calls` payload contracts without updating downstream facts, shape,
  reducer, and docs in the same branch.
- Do not move registry lookup, path normalization, or runtime parser allocation
  into this package.
