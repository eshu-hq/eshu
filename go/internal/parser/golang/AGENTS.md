# AGENTS.md - internal/parser/golang guidance

## Read first

1. `README.md` - package boundary, exported surface, and invariants
2. `doc.go` - godoc contract for the Go adapter package
3. Pick the leaf for the change first, then the file inside it. This package
   is a root plus four one-way-layered leaves (see README.md's Ownership
   boundary): `symbols/` is the bottom layer and never imports a sibling leaf
   or the root; `dataflow/`, `deadcode/` (+ `deadcode/semantic/`), and
   `prescan/` may import `symbols/` but not each other or the root; the root
   imports all four.
   - **Root (`golang/`)** - payload assembly and composition:
     - `language.go`, `call_chain_metadata.go` - `Parse`, payload assembly,
       call metadata, receiver handling, and chained receiver proof
     - `function_literal_reachability.go`, `function_value_references.go` -
       callback/registry literal and function-value root boundaries (the
       identifier and scope logic these lean on now lives in `symbols/`)
     - `embedded_sql.go`, `embedded_shell.go` - SQL and shell call-site
       extraction and line-number accounting
     - `helpers.go`, `types.go` - local forwarders to `parser/shared` and
       shared contract type aliases
     - `aws_sdk_receiver_service.go`, `file_level_indexes.go`,
       `framework_routes.go`, `framework_semantics_gate.go`,
       `parameter_count.go`, `scip_symbols.go` - remaining root-owned payload
       pieces; read each file's own doc comment before changing it
   - **`symbols/`** - identifier, scope, receiver, and variable-type
     resolution; the bottom layer every other leaf depends on:
     - `parent_lookup.go` - `BuildParentLookup`, the per-file child-to-parent
       index used by every helper that walks ancestors; required to keep
       ancestor traversal amortized O(1)
     - `variable_index.go`, `imported_variable_index.go` - per-file,
       per-scope variable-type lookup indices that replace the per-call
       full-tree walks the dead-code and package-prescan helpers used to do
     - `receiver.go`, `receiver_concrete.go`, `variable_scope.go`,
       `variable_types.go`, `map_receiver.go`, `struct_fields.go` - receiver
       and variable typing
     - `exported.go` - `IdentifierIsExported`
     - `helpers.go` - identifier/scope walk helpers relocated from the root
   - **`dataflow/`** - the opt-in control-flow/taint pass
     (`Options.EmitDataflow`, byte-identical when off):
     - `lower.go`, `bindings.go`, `access_paths.go`, `emit.go` - lowers each
       function to a control-flow graph over `internal/parser/cfg`, extracts
       per-statement defs/uses, and emits the `dataflow_functions` bucket
     - `taint_facts.go` - the Go source/sink/sanitizer catalog feeding
       `taint_findings`
     - `effects.go`, `interproc.go` - the Go-AST-to-EffectsSpec extraction
       (params, returns, intra-file call-arg sites) and the per-file
       composition into interprocedural findings over
       `internal/parser/valueflow` and `internal/parser/interproc` (the
       `interproc_findings` bucket)
   - **`deadcode/` and `deadcode/semantic/`** - dead-code root evidence:
     - `deadcode/roots.go` - signature roots, import aliases, and root-kind
       helpers
     - `deadcode/registrations.go` - net/http and Cobra registration evidence
     - `deadcode/semantic/roots.go` - top-level semantic root collection
     - `deadcode/semantic/helpers.go`, `deadcode/semantic/flows.go` -
       interface, callback, field, and argument flow helpers
   - **`prescan/`** - file-local and package-level pre-scan:
     - `prescan.go` - `PreScan`, the cheap name-only walk used by the
       collector import-map prescan
     - `package_interface.go` (formerly `package_interface_prescan.go`) -
       imported-interface parameter extraction; also now owns
       `MethodDeclarationKeys`
     - `package_evidence.go` - package-level interface, method, and generic
       evidence
4. The `go_*_test.go` and `engine_go_rich_semantics_test.go` external-package
   engine tests at the package root (package `golang_test`; see README.md for
   the current file/function counts -- they shifted with the leaf split,
   issue #6774), plus the parent's `TestDefaultEngineParsePathGo` in
   `engine_test.go` and `go_package_interface_prescan_test.go` (which lives in
   the parent directory, not here), before changing emitted payload shape
5. `go_test_helpers_test.go` - `writeGoFixture`, the shared nested-directory
   fixture writer. Cross-file assertions come from `../parsertest`; helpers
   that stay file-local (taint and dataflow row lookups, dogfood corpus
   pickers, the `*testing.B` writer) live beside the tests that use them
6. `engine_data_carriage_return_test.go` - one of the two single-language
   relocations closing out #6062, external package `golang_test`. Pins the Go
   raw-string carriage-return case (issue #6306) via
   `parsertest.MustParsePath`/`parsertest.WriteFile`, since it needs only the
   parent's exported `DefaultEngine`/`Options`/`Engine.ParsePath` surface

## Invariants this package enforces

- Production dependency direction stays one way: parent parser code may import
  this package, but production files and same-package tests here must not import
  `internal/parser`. External `golang_test` files may import the parent only
  for `parser.DefaultEngine`, `parser.Options`, and `parser.Engine`, and never
  reach parent internals.
- Shared assertions come from `go/internal/parser/parsertest` (`WriteFile`,
  `AssertBucketItemByName`, `AssertBucketItemByFieldValue`,
  `AssertStringFieldValue`, `AssertIntFieldValue`, `AssertStringSliceContains`,
  `AssertStringSliceNotContains`, `AssertStringSliceEquals`,
  `AssertFunctionByNameAndClass`, `AssertFrameworksEqual`,
  `AssertNestedStringSliceEqual`, `AssertNestedRouteEntriesEqual`).
  `go_test_helpers_test.go` holds only `writeGoFixture`, which creates parent
  directories before delegating to `parsertest.WriteFile` for the tests that
  lay out multi-package module trees, and `go_parent_lookup_bench_test.go`
  keeps its own `*testing.B` writer because parsertest has none. Do not add
  local copies of parsertest helpers back, and do not export a parent-private
  helper to reach it.
- Internal layering is one-way: `symbols/` imports only `internal/parser/shared`
  and tree-sitter (never a sibling leaf or this root); `dataflow/`,
  `deadcode/` (+ `deadcode/semantic/`), and `prescan/` may import `symbols/`
  but not each other or the root; the root imports all four leaves. A change
  that needs a leaf to import a sibling leaf means the shared piece belongs in
  `symbols/` instead -- see README.md's Ownership boundary.
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

- Add a new Go payload field by writing or updating a focused external-package
  engine test here, then changing `language.go` or the helper that owns the
  evidence.
- Add a new dead-code root by adding a focused test case in `deadcode/` (or
  `deadcode/semantic/` for semantic-root evidence) first, then editing the
  narrow helper that owns that evidence family. Prove a run pin with
  `../scripts/go-test-run-guard.sh 56 TestDefaultEngineParsePathGo -- ./internal/parser/golang -count=1`
  from the `go/` module root; a bare `go test -run` exits 0 on a partial match.
- Add SQL API support by writing a focused `embedded_sql_test.go` case first.
- Change same-package interface evidence by testing both
  `ImportedInterfaceParamMethods` and the parent package pre-scan wrapper.
- Keep compatibility wrappers in the parent parser package thin. New Go
  parsing behavior belongs here unless it is registry, runtime, or content
  metadata wiring.

## Failure modes and how to debug

- Missing Go functions, structs, interfaces, imports, variables, or calls
  usually means `language.go` skipped a tree-sitter node kind or changed bucket
  names. Compare the focused `go_*_test.go` fixture output in this directory.
- False Go CALLS edges for package-qualified or chained calls usually mean
  import alias metadata, `chain_receiver_obj_type`, or
  `chain_receiver_method` drifted from reducer expectations.
- Missing interface implementation roots usually means
  `ImportedInterfaceParamMethods` was not passed back through `Options`, or a
  type-flow helper lost local concrete-type evidence.
- Missing handler or Cobra roots usually means import alias evidence changed in
  `deadcode/roots.go` or registration evidence changed in
  `deadcode/registrations.go`.
- Wrong embedded SQL rows usually mean the call-site matcher or SQL table
  matcher in `embedded_sql.go` became too broad or too narrow.
- Non-deterministic import-map output usually means a helper returned unsorted
  names or iterated a map without sorting.

## Anti-patterns specific to this package

- Importing the parent parser package from production files or same-package
  tests. Keep the external-test exception limited to black-box coverage of the
  public parent engine, without reusing unexported helpers.
- Carrying a discarded type assertion (`x, _ := v.(T)`) into a test helper: a
  present-but-wrongly-typed field then reads as absent and passes a negative
  assertion. The parsertest helpers fail closed; keep it that way.
- Adding JavaScript, Python, Java, SQL, YAML, JSON, or other language behavior
  here.
- Returning partial payloads after a Go parse failure.
- Treating every composite literal, function value, or selector expression as
  live code without bounded evidence.
- Adding telemetry from this package; parse timing belongs to the runtime path
  that invokes the adapter.
- Re-adding per-call full-tree walks for variable-type or ancestor lookups in
  `deadcode/semantic/roots.go`, `prescan/package_interface.go`, or any other
  helper. Use `symbols.BuildParentLookup` and the variable-type index builders
  in `symbols/variable_index.go` / `symbols/imported_variable_index.go` so
  per-file cost stays linear.
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

### #6062 residual

One single-language test remains at the `internal/parser` root:
`engine_swift_symbol_gate_test.go`, destined for `swift/`. The three
`engine_typescript_*` / `engine_tsx_*` files that were listed here moved into
`internal/parser/javascript` under #6062, not into this package.
The 27 `<lang>_language.go` Engine-method glue files stay at root by design.
