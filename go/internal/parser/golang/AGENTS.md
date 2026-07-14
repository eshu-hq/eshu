# AGENTS.md - internal/parser/golang guidance

## Read first

1. `README.md` - package boundary, exported surface, and invariants
2. `doc.go` - godoc contract for the Go adapter package
3. `language.go` and `call_chain_metadata.go` - `Parse`, payload assembly,
   call metadata, receiver handling, and chained receiver proof
4. `prescan.go` - `PreScan`, the cheap name-only walk used by the collector
   import-map prescan
5. `dead_code_roots.go` - signature roots, import aliases, and root-kind
   helpers
6. `dead_code_registrations.go` - net/http and Cobra registration evidence
7. `dead_code_semantic_roots.go` - top-level semantic root collection
8. `dead_code_semantic_helpers.go` and `dead_code_semantic_flows.go` -
   interface, callback, field, and argument flow helpers
9. `function_literal_reachability.go` - callback and registry literal root
   boundaries
10. `package_interface_prescan.go` - imported-interface parameter extraction
11. `parent_lookup.go` - per-file child-to-parent index used by every helper
    that walks ancestors; required to keep ancestor traversal amortized O(1)
12. `variable_type_index.go` and `imported_variable_type_index.go` -
    per-file, per-scope variable-type lookup indices that replace the
    per-call full-tree walks the dead-code and package-prescan helpers used
    to do
13. `embedded_sql.go` - SQL literal extraction and line-number accounting
13a. `cfg_lower.go`, `cfg_bindings.go`, `cfg_emit.go` - opt-in dataflow pass:
    lowers each function to a control-flow graph over `internal/parser/cfg`,
    extracts per-statement defs/uses, and emits the `dataflow_functions` and
    `taint_findings` buckets (gated by `Options.EmitDataflow`, byte-identical
    when off)
13b. `cfg_taint_facts.go` - the Go source/sink/sanitizer catalog and the mapping
    from parsed statements to `internal/parser/taint` facts
13c. `cfg_effects.go`, `cfg_interproc.go` - the Go-AST-to-EffectsSpec extraction
    (params, returns, intra-file call-arg sites) and the per-file composition
    into interprocedural findings over `internal/parser/valueflow` and
    `internal/parser/interproc` (the `interproc_findings` bucket)
14. `helpers.go` and `types.go` - local helper and shared contract aliases
15. Parent tests in `go/internal/parser/go*_test.go` before changing emitted
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
- False Go CALLS edges for package-qualified or chained calls usually mean
  import alias metadata, `chain_receiver_obj_type`, or
  `chain_receiver_method` drifted from reducer expectations.
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
- Re-adding per-call full-tree walks for variable-type or ancestor lookups in
  `dead_code_semantic_roots.go`, `package_interface_prescan.go`, or any other
  helper. Use `goBuildParentLookup`, `goBuildVariableTypeIndex`, or
  `goBuildImportedVariableTypeIndex` so per-file cost stays linear.
- Re-adding a per-call `regexp.MustCompile` in `goIdentifierShadowedBeforeOffset`
  (`embedded_shell.go`). Use `identifierShadowPatternsFor`, which caches the
  compiled shadow-detection regexes per identifier.

## What NOT to change without an ADR

- Do not change parent registry extension ownership for `.go` files.
- Do not change the `embedded_sql_queries`, `dead_code_root_kinds`, or
  `function_calls` payload contracts without updating downstream facts, shape,
  reducer, and docs in the same branch.
- Do not move registry lookup, path normalization, or runtime parser allocation
  into this package.

## Evidence notes

### Shadow-detection regex compile hoist (issue #4874)

`goIdentifierShadowedBeforeOffset` compiled two `regexp.MustCompile` patterns
(short-declaration and var-declaration) per call for the os/exec import
alias. The alias identifier is dynamic in principle but in practice is
virtually always `"exec"` (the default os/exec import name — Go forbids
importing the same package path twice in one file with different aliases),
so `identifierShadowPatternsFor` caches the compiled pair per identifier in a
package-level `sync.Map`, capped at `identifierShadowPatternCacheLimit`
(20,000) distinct identifiers to keep an ingester's long-running memory
bounded across a large multi-repo corpus; beyond the cap it falls back to
compiling per call rather than growing memory unboundedly.
`TestGoIdentifierShadowedBeforeOffsetMatchesDeclarationForms` pins the exact
shadowing decision for the short-declaration, var-declaration,
no-declaration, after-offset, and substring-identifier cases before and after
the change; `TestEmbeddedShellCommandsSkipsShadowedAliasPerFunction` proves
the same invariant through the real `EmbeddedShellCommands` entry point, not
just the helper; `TestGoIdentifierShadowedBeforeOffsetCacheIsConcurrencySafe`
exercises the cache from concurrent goroutines under `-race`. The identical
hoist-to-package-level-cache technique is benchmarked directly on the SCIP
and dbtsql sibling sites in this issue (see `../AGENTS.md#evidence-notes` and
`../dbtsql/AGENTS.md`); this package relies on those measurements plus its
own regression and race tests rather than a separate golang-specific
benchmark.

No-Observability-Change: parse timing remains owned by the ingester and
collector runtime paths that call the parent Engine, per this file's existing
anti-pattern against adding telemetry from this package.
