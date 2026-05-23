# AGENTS.md - internal/parser/java

The Java adapter owns Java syntax and metadata evidence. Use `README.md` and
`doc.go` for the current package contract.

## Read First

1. `README.md` and `doc.go`.
2. `parser.go`, `call_inference.go`, `call_context.go`, and
   `type_inference_helpers.go`.
3. `dead_code_roots.go`, `reflection.go`, `metadata.go`,
   `parser_metadata.go`, and `metadata_parse.go`.
4. `parser_test.go`, `metadata_test.go`, and parent Java tests in
   `go/internal/parser`.

## Mandatory Guardrails

- This package MUST NOT import `go/internal/parser`; parent wrappers own
  registry lookup, runtime construction, path normalization, and Engine
  signatures.
- Callers own the tree-sitter parser and must configure it for Java before
  `Parse` or `PreScan`.
- Preserve parent payload buckets for Java declarations, imports, variables,
  calls, roots, reflection rows, and the `java_metadata` compatibility payload.
- Local variables emit only when `Options.VariableScope` normalizes to `all`;
  module scope remains the default.
- Reflection, ServiceLoader, Spring, and method-reference evidence must be
  static and source-backed. Dynamic strings, comments, naming conventions,
  invalid class names, and duplicate simple receiver names are not graph truth.
- Ordering follows source line and name through shared bucket sorting.
- This package emits no runtime telemetry directly.

## Change Scope

- Public parse behavior starts with parent `Engine.ParsePath` tests.
- Internal receiver, argument, class-context, and return-type inference starts
  with child-package tests.
- Dead-code root changes need positive and negative parser tests so ordinary
  methods do not become roots by name.
- Metadata file changes start in `metadata_test.go`.
- Do not change payload bucket names, parent Engine signatures, or dead-code
  semantics without downstream fixture/query impact review and
  architecture-owner approval.
