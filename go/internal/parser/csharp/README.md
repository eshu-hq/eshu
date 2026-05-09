# C# Parser

This package owns C# language extraction that can run without importing the
parent `internal/parser` package. The parent engine still owns registry
dispatch and tree-sitter runtime setup.

Exports:

- `Parse` extracts C# declarations, imports, calls, inheritance metadata, and
  common parser payload fields.
- `PreScan` returns deterministic names for import-map pre-scan.
