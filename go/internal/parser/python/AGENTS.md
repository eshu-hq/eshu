# AGENTS.md - internal/parser/python guidance

## Read first

1. README.md - package boundary, parser surface, and invariants
2. doc.go - godoc contract for the Python adapter package
3. language.go - Parse, PreScan, payload bucket assembly, and tree-sitter walk
4. shared_helpers.go - allowed shared helper imports and copied wire-shape helpers
5. notebook.go and notebook_temp.go - notebook extraction and temporary source file lifecycle
6. dead_code_roots.go, lambda_roots.go, public_api_roots.go - dead-code root evidence
7. semantics.go, imports.go, call_inference.go, annotation_support.go - metadata helpers
8. notebook_test.go and language_test.go - child package contract coverage

## Invariants this package enforces

- Dependency direction stays one way: the parent parser package may import this
  package, but this package must not import internal/parser.
- Parse receives a caller-owned tree-sitter parser and must not close it.
- NotebookSource only keeps executable code cells. Markdown, raw, malformed,
  and blank cells do not become parser input.
- Invalid notebook JSON returns an error instead of partial source.
- SAM and serverless Lambda roots only mark handlers when config declares a
  Python runtime.
- Parent Engine signatures stay in go/internal/parser/python_language.go.

## Common changes and how to scope them

- Add parse behavior with a failing test in language_test.go or a parent
  engine_python_* test before editing adapter code.
- Add notebook behavior with a focused test in notebook_test.go first.
- Keep generic parser helpers in the shared parser package when multiple
  language adapters need them.
- Keep Python-only evidence here instead of adding new parent helper branches.

## Failure modes and how to debug

- Missing notebook functions or classes usually means code cells were skipped
  or cell source was joined incorrectly. Reproduce the notebook shape in
  notebook_test.go.
- Missing Lambda roots usually means config discovery did not walk to the repo
  root, YAML templating was not sanitized, or the handler path did not resolve
  to the parsed source file.
- Missing call receiver metadata usually starts in call_inference.go; check
  class context and constructor assignment scope before changing payload shape.
- Missing public API roots usually starts in public_api_roots.go; compare the
  package __init__.py import form to the parsed source path.

## Anti-patterns specific to this package

- Importing the parent parser package to reuse unexported helpers.
- Closing the tree-sitter parser passed to Parse.
- Treating markdown or raw notebook cells as Python code.
- Marking non-Python SAM or serverless handlers as Python dead-code roots.
- Moving registry dispatch or Engine method signatures into this package.

## What NOT to change without an ADR

- Do not change parser payload bucket names or wire fields without updating the
  query and docs contract.
- Do not make Python Lambda config discovery unbounded outside the repository
  root.
- Do not add cross-language helper dependencies here when the shared parser
  package is the correct boundary.
