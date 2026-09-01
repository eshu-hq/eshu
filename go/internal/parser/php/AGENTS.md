# AGENTS.md - internal/parser/php guidance

## Read first

1. README.md - package boundary and PHP context invariants
2. doc.go - godoc contract for the PHP adapter
3. parser.go - two-pass AST walk, payload assembly, namespace, dead-code staging
4. declarations.go - class/interface/trait/anonymous/function/property nodes
5. imports.go - namespace use declaration, group, and alias handling
6. call_emit.go - call and variable context resolution and emission
7. calls.go - call row, receiver, and deduplication helpers
8. alias.go - receiver and return-type inference helpers
9. returns.go - return chain and parenthesized-receiver helpers
10. dead_code_roots.go - AST dead-code facts and root classification
11. support.go - base, type, parameter, and return-type AST helpers

## Invariants this package enforces

- Dependency direction stays one way: parent parser code may import this
  package, but production code and `package php` tests must not import
  internal/parser. The `php_*_test.go` files (18 files, external package
  `php_test`, relocated from the parent by #6062) are the narrow exception:
  they import the parent only for `parser.DefaultEngine` and `parser.Options`,
  and never reach parent internals.
- Parse preserves the legacy PHP payload shape, including traits, interfaces,
  namespace, context metadata, aliases, and receiver inference. The migration
  from the line scanner to the tree-sitter AST kept the payload byte-identical;
  the `php_*_test.go` parity tests in this directory pin that contract, together
  with `TestDefaultEngineParsePathPHPFixtures` and the cyclomatic-complexity
  cases that stay in the parent because they sweep several languages.
- Shared assertions come from `go/internal/parser/parsertest`
  (`WriteFile`, `AssertBucketItemByName`, `AssertBucketItemByFieldValue`,
  `AssertStringFieldValue`,
  `AssertStringSliceContains`, `AssertStringSliceEquals`,
  `AssertFunctionByNameAndClass`, `AssertFrameworksEqual`,
  `AssertNestedRouteEntriesEqual`). `php_test_helpers_test.go` holds only what
  parsertest lacks; do not add local copies of parsertest helpers back, and do
  not export a parent-private helper to reach it.
- PreScan uses a declaration-only AST walk so parent pre-scan and full parse
  agree on declaration names without running full semantic extraction twice.
- Phase 1 collects declarations, imports, type evidence, dead-code facts, and
  candidate route attributes; phase 2 emits variables and calls so
  cross-statement inference sees the whole file. Do not collapse the two
  passes: phase 2 depends on whole-file type evidence phase 1 only finishes
  collecting after seeing the entire file.
- Route-attribute resolution (`buildPHPFrameworkSemantics`,
  `phpSymfonyRoutes`) consumes the candidate nodes phase 1 recorded while
  visiting `attribute` nodes for `observePHPAttribute`; it does not walk the
  tree itself. Do not reintroduce a dedicated route walk — extend the phase 1
  `attribute` case and the post-walk resolution instead.

## Common changes and how to scope them

- Add PHP evidence by writing a focused external `php_test` test in this
  directory first (a new `php_*_test.go` file or a case in the matching family
  file) unless an in-package contract test already covers the behavior.
  Prove a run pin with `../scripts/go-test-run-guard.sh 55 TestDefaultEngineParsePathPHP -- ./internal/parser/php -count=1`
  from the `go/` module root; a bare `go test -run` exits 0 on a partial match.
- Confirm node kinds with a compiled-grammar probe (parse a snippet and dump
  `node.Kind()`), not a filtered search of the grammar source.
- Keep registry, Engine dispatch, runtime loader, and content-shape changes
  outside this package unless the task explicitly includes those files.
- Use internal/parser/shared helpers for payload buckets, sorting, source reads,
  and node text/line access.

## Failure modes and how to debug

- Missing class_context metadata usually means call_emit.go context resolution
  changed, or a declaration node kind is no longer matched.
- Missing inferred_obj_type values usually mean alias.go receiver inference or
  returns.go chained reference resolution changed.
- Missing call rows usually mean call_emit.go skipped a member, nullsafe,
  scoped, constructor, or function call node the parent tests rely on.
- Missing dead-code roots usually mean dead_code_roots.go stopped observing a
  route array, attribute, or hook node.

## Anti-patterns specific to this package

- Importing the parent parser package from production code or a `package php`
  test. Only the external `php_test` files may, and only for the engine API.
- Reintroducing line-oriented or regex symbol extraction; all extraction is
  AST node-walking.
- Emitting new bucket keys without matching downstream shape work.

## What NOT to change without an ADR

- Do not change PHP extension ownership or registry behavior from this package.
- Do not change the tree-sitter PHP grammar pin without re-running the
  `php_*_test.go` parity tests here, the parent's
  `TestDefaultEngineParsePathPHPFixtures`, and the `php_comprehensive` golden
  gate.
