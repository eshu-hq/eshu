# AGENTS.md - internal/parser/dart guidance

## Read first

1. README.md - package boundary and payload buckets
2. doc.go - godoc contract for the Dart adapter
3. parser.go - public parser and pre-scan entrypoints
4. syntax_index.go - tree-sitter declaration extraction
5. calls.go - AST call-site detection (dartCallChain, function_calls rows),
   folded into syntax_index.go's single collect traversal (#5350)
6. parser_test.go - behavior coverage for payload shape
7. dogfood_real_repo_test.go - standing real-repo-validated snapshot test
   (#5399); do not edit testdata/dogfood_real_repo_snapshot.txt by hand,
   regenerate with `DOGFOOD_UPDATE_SNAPSHOT=1 bash scripts/dogfood-dart.sh`

## Invariants this package enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import internal/parser.
- Parse preserves the legacy Dart payload shape and deterministic bucket order.
- Parent engine paths must call ParseWithParser and PreScanWithParser with the
  runtime-owned Dart parser instead of constructing a new runtime per file.
- `dead_code_root_kinds` must stay syntax-local: top-level `main`,
  constructors, `@override`, Flutter `build`/`createState`, and public `lib/`
  declarations outside `lib/src/`.
- PreScan derives names from Parse so the parent engine sees the same language
  evidence in both phases.

## Common changes and how to scope them

- Add Dart evidence by writing a focused test in parser_test.go first.
- Keep file reading in this package through internal/parser/shared.ReadSource.
- Keep registry, Engine dispatch, and content-shape changes outside this
  package unless the task explicitly includes those files.

## Failure modes and how to debug

- Missing declarations or root metadata usually mean the syntax index is not
  covering the relevant Dart grammar node shape.
- Duplicate or unstable call rows usually mean the `full_name` dedup in
  `appendUniqueDartCall` or bucket sorting changed.
- A missing call site or a call misclassified as a declaration (or vice
  versa) usually means a new Dart grammar shape needs a case in
  `dartCallChain.observe` (see calls.go's node-kind switch). Call detection
  runs inside `dartSyntaxIndex.collect`; a call site that only appears inside a
  parameter default or annotation depends on collect's calls-only descent into
  signature subtrees.
- Duplicate import rows usually mean wrapper and concrete import/export nodes
  are both being emitted.
- A `TestDogfoodDartRealRepoSnapshot` mismatch means a code change altered
  bucket counts for `tests/fixtures/dogfood/dart_real_repo`; verify the delta
  is intended, then regenerate with `DOGFOOD_UPDATE_SNAPSHOT=1 bash
  scripts/dogfood-dart.sh`.

## Anti-patterns specific to this package

- Importing the parent parser package.
- Bypassing the parent runtime parser path for engine parse or pre-scan calls.
- Emitting new bucket keys without matching downstream shape work.

## What NOT to change without an ADR

- Do not change Dart extension ownership or registry behavior from this package.
