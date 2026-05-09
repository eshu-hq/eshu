# Scala Parser

This package owns Scala language extraction that can run without importing the
parent `internal/parser` package. The parent engine still owns registry
dispatch and tree-sitter runtime setup.

Exports:

- `Parse` extracts Scala classes, objects, traits, functions, variables,
  imports, and calls.
- `PreScan` returns deterministic names for import-map pre-scan.
