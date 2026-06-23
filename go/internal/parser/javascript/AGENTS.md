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
  once and cached. Output is identical for valid code, proven by the unchanged
  `engine_javascript_*`, `engine_typescript_*`, `engine_tsx_*` tests and the
  js/ts/tsx comprehensive golden fixtures. Two framework-semantics buckets are
  intentionally narrowed because the prior raw-source regexes matched code-shaped
  tokens inside comments, strings, imports, and type annotations: `react.hooks_used`
  now collects only real `call_expression` hook calls (bare and `React.useState`
  member form), and `aws`/`gcp` `client_symbols` collects only `new`-constructed
  `XxxClient` names. Dynamic `import("@aws-sdk/client-*")` is now covered for the
  service buckets alongside static import and require. These narrowings drop prior
  false positives and have regression tests in
  `engine_javascript_ast_conversion_test.go`.
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
- Calling `node.Parent()` to recover ancestor context in any helper that runs
  per declaration node. Tree-sitter's `Parent()` crosses cgo into
  `ts_node_parent` and re-walks from the root, so per-node Parent loops scale as
  O(n_declarations * depth) cgo crossings and dominated parse CPU in #3586. Use
  the per-parse `javaScriptParentLookup` (`parent_lookup.go`): `Parse` builds it
  once, threads it through `javaScriptDeadCodeEvidence.parents` and the helper
  signatures, and helpers walk ancestors via `parents.parent(node)`. Keep the
  cgo-crossing regression gate
  `TestJavaScriptParentLookupEliminatesCgoCrossings` green.

## What NOT to change without an ADR

- Do not change `.js`, `.jsx`, `.ts`, `.tsx`, `.mts`, `.cts`, `.mjs`, or `.cjs`
  registry ownership from the parent parser without an ADR.
- Do not add backend, collector, reducer, query, or storage dependencies here.

## Residual-regex audit — permanent exceptions (issue #3590, epic #3531)

Three regex patterns remain in this package after the JS/TS/TSX regex-to-AST
migration (#3539/#3563). Each was audited and confirmed as a justified permanent
exception: none performs primary symbol or entity extraction over raw source. All
three operate only on a string value already isolated by the AST. Adding any
additional within-string regex requires an ADR.

### `javaScriptStaticComputedMemberNameRe` — `javascript_names.go`

**Category:** content-classification over AST node text (computed-property
shape validator).

**Justification:** This regex validates the shape of an unquoted string that
`javaScriptComputedPropertyName` has already extracted from a
`computed_property_name` AST node. It accepts simple identifiers, dotted member
chains (`foo.bar.baz`), and decimal integer literals; it rejects anything that
cannot be a static property name (binary expressions, template substitutions,
calls, etc.). It is a post-AST filter on already-isolated node text — not a
source scanner — so it cannot produce false positives from tokens inside
comments, string literals, or type annotations.

**Call site:** `javaScriptComputedPropertyName` in `javascript_names.go`, called
only after `javaScriptStaticComputedPropertyName` has returned `false` and the
bracket-expression inner text has been unquoted from an AST node.

**Migration verdict:** No migration needed. The grammar does not model the
distinction between a static and a dynamic computed property as separate node
types; the validator must run over the extracted string value, which is already
AST-isolated.

### `javaScriptAWSClientServiceRe` — `javascript_semantics_ast.go`

**Category:** content-classification over AST-isolated package-specifier string
(slug extraction from an import module-specifier).

**Justification:** This regex extracts the service slug from
`@aws-sdk/client-<slug>` package specifiers. It receives the string value of an
`import_statement` source or `require`/`import(...)` argument node — a string
already isolated by `javaScriptImportModuleSpecifiers`, which walks the AST. The
regex never sees raw source bytes. The grammar models the module specifier as a
single `string` node; there is no tree-sitter node type that corresponds to the
slug suffix of a scoped npm package name, so string content matching is
structurally required.

**Call site:** `javaScriptImportServiceSlugs` in `javascript_semantics_ast.go`,
called from `detectAWSSemantics` in `javascript_semantics_helpers.go`, which
feeds the `aws.services` payload bucket.

**Migration verdict:** No migration needed. The target is the textual content of
a string literal whose structure the grammar cannot further decompose into
provider/slug sub-nodes. The extraction already runs on the AST boundary.

### `javaScriptGCPServiceRe` — `javascript_semantics_ast.go`

**Category:** content-classification over AST-isolated package-specifier string
(slug extraction from an import module-specifier).

**Justification:** Identical exception category to `javaScriptAWSClientServiceRe`.
This regex extracts the service slug from `@google-cloud/<slug>` specifiers. It
receives its input from `javaScriptImportModuleSpecifiers`, which has already
walked the AST to isolate each import/require string node value.

**Call site:** `javaScriptImportServiceSlugs` in `javascript_semantics_ast.go`,
called from `detectGCPSemantics` in `javascript_semantics_helpers.go`, which
feeds the `gcp.services` payload bucket.

**Migration verdict:** No migration needed. Same structural argument as the AWS
regex: the grammar has no sub-node type for the slug portion of a scoped package
name.

### Characterization tests

All three exceptions are pinned by
`javascript_residual_regex_characterization_test.go` (package `javascript`).
Coverage includes:

- `javaScriptStaticComputedMemberNameRe`: acceptance of identifiers, dotted
  chains, and decimal integers; rejection of binary expressions, calls, template
  substitutions, leading zeros, hyphens, and empty strings; plus a wrapper-path
  test that drives the production helper `javaScriptComputedPropertyName` over
  real `computed_property_name` nodes (class-method and object-literal keys),
  covering the static string/number/concat cases, the dotted-member-chain case
  that exercises the residual regex (`[Symbol.iterator]` → `Symbol.iterator`),
  and the dynamic cases the wrapper must reject (`[getName()]`, template
  substitution).
- `javaScriptAWSClientServiceRe`: slug extraction for `s3`, `dynamodb`,
  `rds-data`, `secrets-manager`, `ssm`; rejection of `lib-*` packages, empty
  slugs, v2 bare names, GCP specifiers, uppercase slugs; integration over static
  `import` and `require` via `javaScriptImportServiceSlugs`; deduplication.
- `javaScriptGCPServiceRe`: slug extraction for `storage`, `bigquery`, `pubsub`,
  `datastore`, `logging-min`; rejection of AWS specifiers, unscoped names, empty
  slugs, uppercase slugs; integration over `require` and static `import`;
  cross-pattern isolation confirming AWS and GCP regexes are mutually exclusive.

### No-Regression Evidence (issue #3590)

```
cd go && gofmt -l internal/parser/javascript/   # empty — no formatting drift
cd go && go test ./internal/parser/... -count=1  # 1238 passed, 41 packages
cd go && golangci-lint run ./internal/parser/javascript/...  # no issues
```

### No-Observability-Change

This package emits no telemetry by design. The audit adds only tests and
documentation; no spans, metrics, or logs are added or removed.
