# AGENTS.md - internal/parser/cpp guidance

## Read First

1. README.md - package boundary and parser responsibilities
2. doc.go - godoc contract
3. parser.go - C++ payload extraction
4. dead_code_roots.go - C++ parser-backed dead-code root metadata
5. qualified_method.go - AST extraction of out-of-line `Class::method` names
6. header_roots.go - bounded direct local-header public API roots
7. helpers.go - local helper functions copied out of the parent package

## Invariants This Package Enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import internal/parser.
- The caller owns tree-sitter parser construction and closing.
- Parse and PreScan must preserve the parent engine payload shape.
- Dead-code roots are parser evidence only. Keep `cpp.public_header_api`
  bounded to headers directly included by the current file and inside the
  repository root.
- Out-of-line `Class::method` name and class context come from tree-sitter
  declarator fields (`cppQualifiedFunctionNameAndClassFromNode` in
  `qualified_method.go`), not a regex over node text. Do not reintroduce a
  text-scan extractor for primary symbol naming.

## Regex Audit (issue #3574)

This is the within-node regex audit for the cpp parser. Each site is either
migrated to AST or a documented justified exception.

Migrated to AST (regex removed):

- `cppQualifiedFunctionPattern` -> `cppQualifiedFunctionNameAndClassFromNode`
  (`qualified_method.go`). The old regex scanned `function_definition` node text
  for `Class::method` and split on `::`. The AST extractor reads the declarator
  fields (`function_declarator` -> `qualified_identifier` -> `scope`/`name`),
  walks nested qualifiers to the innermost scope, unwraps pointer/reference
  return declarators, and strips template argument lists from the scope. It is
  byte-parity on simple, destructor, and namespace-nested definitions and also
  recovers operator overloads (`Class::operator==`) and template-qualified
  methods (`Box<T>::get`) that the regex dropped.

Documented within-node text fallbacks (kept, run only on an already-located
declaration node, never on whole-file source):

- `cppFunctionPointerAliasPattern` - function-pointer alias names from a
  declaration node (`using Alias = R (*)(...)`, `typedef R (*Alias)(...)`).
- `cppDirectInitializerTargetPattern` - bare initializer identifier a
  function-pointer declarator points at (call-site/initializer evidence).
- `cppBraceInitializerPattern` - brace-initializer entries for function-pointer
  tables (initializer evidence).
  Each is owner-confirmation dead-code-root evidence over one declaration node;
  the tree-sitter declarator nesting varies by alias spelling, so recovering the
  identifier from the bounded node text is the documented fallback.

Documented external header-text exceptions (kept, scan bytes of directly
included local headers that are intentionally not parsed):

- `cppFreeHeaderPrototypePattern`, `cppClassBlockPattern`,
  `cppClassMethodPrototypePattern`, `cppBlockCommentPattern`,
  `cppLineCommentPattern` in `header_roots.go`. `AnnotatePublicHeaderRoots`
  reads external `.h` files via `os.ReadFile`; those headers are not part of the
  current translation unit's tree-sitter parse, so there is no AST to read. The
  scan is bounded: only directly `#include`d local headers inside the repository
  root, no transitive include resolution, no macro expansion, no header parse.
  Migrating these would require parsing each header and is out of scope without
  an ADR.

## No-Regression / No-Observability-Change

- No-Regression Evidence: the AST extractor replaces a node-text regex with a
  field walk over the already-built tree. Output is identical for the simple,
  destructor, and namespace-nested cases proven by the unchanged
  `cpp_dead_code_roots_test.go` (including
  `TestDefaultEngineParsePathCPPMarksNamespaceQualifiedHeaderMethod`, which pins
  `api::Service::run` to class `Service`). New parity and improvement coverage
  lives in `internal/parser/cpp/dead_code_roots_test.go`
  (`TestCPPQualifiedFunctionNameAndClassFromNode`, 10 cases incl. operator,
  template, pointer/reference return) and
  `internal/parser/cpp_qualified_method_ast_test.go`
  (`TestDefaultEngineParsePathCPPOutOfLineQualifiedMethodsViaAST`, end-to-end via
  `ParsePath`). Commands and counts:
  - `cd go && gofmt -l internal/parser/cpp` -> empty.
  - `cd go && go test ./internal/parser/... -count=1` -> 1182 passed, 41
    packages.
  - `cd go && golangci-lint run ./internal/parser/...` -> No issues found.
  - `command rg -c 'regexp\.MustCompile' internal/parser/cpp/*.go` ->
    dead_code_roots.go 3, header_roots.go 5, parser.go 1 (cppQualifiedFunctionPattern
    removed; `cTypedefAliasPattern` in parser.go is out of #3574 scope).
- No-Observability-Change: this package emits no telemetry by design; the audit
  neither adds nor removes spans, metrics, or logs.

## Common Changes And Scope

- Add C++ parser behavior by starting with focused parser tests in the parent
  parser package or this package.
- Add dead-code root behavior by proving the parser metadata first, then the
  query suppression and maturity response.
- Keep registry dispatch and runtime parser lookup in the parent parser package.
- Keep shared cross-language primitives in internal/parser/shared.

## Anti-Patterns

- Importing the parent parser package.
- Moving registry or engine dispatch into this package.
- Changing payload keys without updating downstream parser tests.
- Following transitive include graphs, expanding macros, or resolving build
  targets inside this parser package without an ADR-backed design.
