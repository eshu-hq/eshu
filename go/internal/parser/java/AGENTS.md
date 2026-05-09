# AGENTS.md - internal/parser/java guidance

## Read first

1. `README.md` - package boundary, exported surface, and invariants
2. `doc.go` - godoc contract for `Parse`, `PreScan`, `ParseMetadata`,
   `ClassReference`, and `MetadataClassReferences`
3. `parser.go` - payload assembly, declaration traversal, imports, and calls
4. `call_inference.go`, `call_context.go`, and `type_inference_helpers.go` -
   Java receiver, argument, class-context, and return-type inference
5. `dead_code_roots.go` and `reflection.go` - Java root classification and
   literal reflection references
6. `metadata.go` and `parser_metadata.go` - ServiceLoader, Spring metadata,
   decorators, parameter types, and method-reference target evidence

## Invariants this package enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import `go/internal/parser`.
- `Parse` preserves the parent payload contract for `functions`, `classes`,
  `interfaces`, `annotations`, `enums`, `variables`, `imports`, and
  `function_calls`; `ParseMetadata` preserves the `java_metadata`
  compatibility payload.
- The caller owns the tree-sitter parser and must configure it for Java before
  calling `Parse` or `PreScan`.
- Local variables are emitted only when `Options.VariableScope` normalizes to
  `all`; module scope remains the default.
- Reflection and metadata evidence stay static. Dynamic strings and invalid
  class names must not become graph evidence.
- Ordering follows source line, then name, through shared bucket sorting.

## Common changes and how to scope them

- Add Java syntax payload fields in `parser.go` with a parent engine test first
  when the contract is visible through Engine ParsePath.
- Add receiver or argument inference in `call_inference.go`,
  `call_context.go`, or `type_inference_helpers.go` with a child-package unit
  test when the helper contract is internal.
- Add dead-code roots in `dead_code_roots.go` with positive and negative parent
  parser tests so ordinary methods do not become roots by name alone.
- Add reflection support in `reflection.go` only for literal, statically named
  evidence.
- Add a metadata file shape by extending `metadata.go` and `metadata_test.go`
  first.

## Failure modes and how to debug

- Missing Java symbols usually mean the tree-sitter kind changed or the walk in
  `parser.go` missed a declaration kind.
- Missing receiver types usually mean the declaration was outside the scope
  indexed by `buildJavaCallInferenceIndex`.
- Extra dead-code roots usually mean annotation, framework, or hook matching in
  `dead_code_roots.go` accepted a broad shape.
- Missing ServiceLoader roots usually mean the path classifier in `metadata.go`
  did not match the resource path.
- Wrong line numbers usually mean helper code used the enclosing node instead
  of the declaration or literal node that proves the evidence.

## Anti-patterns specific to this package

- Importing the parent parser package to reuse private helpers.
- Adding backend, collector, reducer, or graph storage dependencies.
- Emitting graph truth from dynamic Java strings, comments, or naming
  conventions without source evidence.
- Keeping compatibility wrappers in this package; parent Engine signatures
  remain in `go/internal/parser/java_language.go`.

## What NOT to change without an ADR

- Do not change the Java payload bucket names or parent Engine method
  signatures.
- Do not change Java dead-code root semantics without fixture evidence and
  query-surface impact review.
- Do not add runtime telemetry directly here unless the parser telemetry
  contract moves out of the parent engine.
