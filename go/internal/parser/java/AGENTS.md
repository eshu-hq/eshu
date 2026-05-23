# AGENTS.md - internal/parser/java

## Read First

1. `README.md` and `doc.go`.
2. `parser.go`, `call_inference.go`, `call_context.go`, and
   `type_inference_helpers.go`.
3. `dead_code_roots.go`, `reflection.go`, `metadata.go`,
   `parser_metadata.go`, and `metadata_parse.go`.
4. `parser_test.go`, `metadata_test.go`, and parent Java tests in
   `go/internal/parser`.

## Guardrails

- MUST NOT import `go/internal/parser`; parent wrappers own
  registry lookup, runtime construction, path normalization, and Engine
  signatures.
- MUST let callers own the tree-sitter parser and configure it for Java before
  `Parse` or `PreScan`.
- MUST preserve parent payload buckets for Java declarations, imports, variables,
  calls, roots, reflection rows, and the `java_metadata` compatibility payload.
- MUST emit local variables only when `Options.VariableScope` normalizes to `all`;
  module scope remains the default.
- MUST keep reflection, ServiceLoader, Spring, and method-reference evidence
  static and source-backed. Dynamic strings, comments, naming conventions,
  invalid class names, and duplicate simple receiver names are not graph truth.
- MUST sort by source line and name through shared bucket sorting.
- MUST emit no runtime telemetry directly.

## Change Scope

- Start public parse behavior with parent `Engine.ParsePath` tests.
- Start internal receiver, argument, class-context, and return-type inference
  with child-package tests.
- Dead-code root changes need positive and negative parser tests so ordinary
  methods do not become roots by name.
- Metadata file changes start in `metadata_test.go`.
- Do not change payload bucket names, parent Engine signatures, or dead-code
  semantics without downstream fixture/query impact review and
  architecture-owner approval.
