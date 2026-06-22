# AGENTS.md - internal/parser/javascript guidance

## Read first

1. README.md - package boundary, parser ownership, and invariants
2. doc.go - godoc contract for the JavaScript parser package
3. javascript_language.go - `Parse`, `PreScan`, payload construction, and
   tree-sitter traversal
4. cfg_emit.go - opt-in value-flow buckets (EmitDataflow) over jsdataflow and the
   shared internal/parser/dataflowemit renderer
5. javascript_imports.go and javascript_exports.go - import, require, and
   re-export rows
5. javascript_dead_code_roots.go and related `javascript_dead_code_*` files -
   parser-proven dead-code root evidence
6. javascript_semantics.go and javascript_semantics_helpers.go - framework and
   component semantics
7. tsconfig.go - JSONC parsing, path alias resolution, and repository bounds
8. package_json.go - nearest package.json roots and public source targets
9. tsconfig_test.go - behavior coverage for JSONC, path aliases, and candidate
   ordering
10. package_json_test.go - behavior coverage for nearest package ownership and
   package public source mapping

## Invariants this package enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import internal/parser.
- `Parse` receives a `ParserFactory` from the parent wrapper. Do not pass or
  store parent Engine values here.
- Payload buckets must stay deterministic. Sort named buckets before returning
  and do not iterate maps directly into output rows.
- TSConfigImportResolver never resolves outside the repository root.
- PackageFileRootKinds and PackagePublicSourcePaths use nearest package.json
  ownership, so nested packages are not claimed by workspace-root manifests.
- JSONC syntax in tsconfig.json is valid input. Do not replace the JSONC
  cleanup with plain encoding/json on raw bytes.
- Dead-code root kinds must be syntax-backed or bounded by package/tsconfig
  files. Do not mark broad public names as roots without parser evidence.
- Symbol, edge, and framework-metadata extraction is tree-sitter AST
  node-walking. Do not reintroduce regex or string scanning for primary
  extraction (method kinds, embedded shell, Hapi/Express routes, Next.js route
  surface, JSX-return detection, AWS/GCP imports, TypeScript public-API
  re-export/import/declaration surface). Sibling dead-code files are parsed
  through the `ParserFactory` via `javaScriptSiblingParser`, which caches roots
  per `Parse` call and only parses non-empty existing files.
- Only three within-string-content regexes are allowed, each running solely
  against a string-literal value already isolated by the AST, never as a source
  scanner: `javaScriptStaticComputedMemberNameRe`
  (`javascript_names.go`, unquoted computed-property validation);
  `javaScriptAWSClientServiceRe` / `javaScriptGCPServiceRe`
  (`javascript_semantics_ast.go`, slug extraction from an AST-isolated import
  specifier). Client-symbol and hook-call extraction uses AST node walks
  (`javaScriptClientSymbolNames`, `javaScriptHookCallNames` in
  `javascript_semantics_ast.go`). Adding any other regex for extraction
  requires an ADR.

## No-Regression / No-Observability-Change

- No-Regression Evidence: the AST conversion replaces multi-pass regex scans
  with single-pass walks over an already-built tree; sibling files are parsed
  once and cached. Output is byte-for-byte identical, proven by the unchanged
  `engine_javascript_*`, `engine_typescript_*`, `engine_tsx_*` tests and the
  js/ts/tsx comprehensive golden fixtures.
- No-Observability-Change: this package emits no telemetry by design; the
  conversion neither adds nor removes spans, metrics, or logs.

## Common changes and how to scope them

- Add parser behavior by writing a focused parent parser test first when the
  public Engine.ParsePath contract is the behavior under test.
- Add tsconfig behavior by writing a focused test in tsconfig_test.go first.
- Add package.json behavior by writing a focused test in package_json_test.go
  first.
- Keep parent wrapper edits limited to signature preservation and shared option
  conversion.
- Keep map payload keys aligned with `internal/content/shape` and existing
  parent parser tests.
- Split files before they approach 500 lines.

## Failure modes and how to debug

- Parser behavior missing from Engine.ParsePath usually points at the parent
  wrapper, registry dispatch, or runtime language name before child traversal.
- Missing resolved_source metadata usually means the nearest tsconfig.json was
  not found, baseUrl resolved outside the repo, or the candidate file does not
  exist.
- Incorrect resolution in workspaces usually points at nearest-config lookup.
  Add a fixture with nested tsconfig.json files before changing lookup order.
- Package roots leaking from a workspace root into a nested package usually
  means nearest package lookup changed. Reproduce it in package_json_test.go.
- Nondeterministic imports usually mean a map was iterated directly. Collect
  candidates, deduplicate explicitly, and preserve a stable order.

## Anti-patterns specific to this package

- Importing the parent parser package to reuse payload helpers.
- Accepting parent Options or Engine types instead of shared parser types.
- Resolving absolute aliases or paths outside repoRoot.
- Marking every exported TypeScript symbol live without package or re-export
  evidence.

## What NOT to change without an ADR

- Do not change `.js`, `.jsx`, `.ts`, `.tsx`, `.mts`, `.cts`, `.mjs`, or `.cjs`
  registry ownership from the parent parser without an ADR.
- Do not add backend, collector, reducer, query, or storage dependencies here.
