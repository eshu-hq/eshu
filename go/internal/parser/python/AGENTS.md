# AGENTS.md - internal/parser/python guidance

## Read first

1. README.md - package boundary, notebook behavior, and invariants
2. doc.go - godoc contract for the Python helper package
3. notebook.go - Jupyter notebook JSON to Python source extraction
4. notebook_test.go - behavior coverage for code-cell extraction and invalid
   JSON

## Invariants this package enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import internal/parser.
- NotebookSource only keeps code cells. Markdown, raw, malformed, and blank
  cells do not become parser input.
- Invalid notebook JSON returns an error instead of partial source.

## Common changes and how to scope them

- Add notebook behavior by writing a focused test in notebook_test.go first.
- Keep temporary-file creation and cleanup in the parent parser package.
- Keep tree-sitter Python parsing out of this package until the shared node
  helper boundary has a separate design.

## Failure modes and how to debug

- Missing notebook functions or classes usually means code cells were skipped
  or cell source was joined incorrectly. Reproduce the notebook shape in
  notebook_test.go.
- Parse errors after notebook conversion usually mean the extracted source no
  longer matches the notebook's executable cell text.

## Anti-patterns specific to this package

- Importing the parent parser package to reuse payload helpers.
- Creating temporary files here. The parent parser owns lifecycle cleanup.
- Treating markdown or raw notebook cells as Python code.

## What NOT to change without an ADR

- Do not move the full Python tree-sitter adapter here until shared tree
  helpers, payload helpers, and registry wiring have explicit package
  contracts.
