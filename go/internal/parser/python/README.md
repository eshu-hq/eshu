# Python Parser Helpers

## Purpose

This package owns Python parser helpers that do not need tree-sitter nodes or
parent parser payload helpers. The first helper converts Jupyter notebook JSON
into plain Python source text by keeping executable code cells and dropping
markdown or empty cells.

## Ownership boundary

The package is responsible for typed Python evidence that can be computed from
bytes and strings. The parent parser package still owns file I/O, temporary
file lifecycle, tree-sitter parsing, payload assembly, Python dead-code roots,
imports, and bucket sorting.

## Exported surface

The godoc contract is in doc.go. Current exports are NotebookSource.

## Dependencies

This package imports only the Go standard library. It must not import the
parent parser package, collector packages, graph storage, or reducer code.

## Telemetry

This package emits no telemetry. Parse timing remains owned by the parent
parser engine.

## Gotchas / invariants

NotebookSource returns an empty string for notebooks without code cells. Invalid
JSON returns an error so the parent parser can fail the file instead of indexing
partial source.

Notebook cells may represent `source` as one string or as an array of strings.
Both forms must preserve line breaks exactly enough for tree-sitter to parse the
same code a notebook user sees.

## Related docs

- docs/plans/2026-05-09-parser-language-layout.md
