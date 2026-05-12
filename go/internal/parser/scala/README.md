# Scala Parser

This package owns Scala language extraction that can run without importing the
parent `internal/parser` package. The parent engine still owns registry
dispatch and tree-sitter runtime setup.

Exports:

- `Parse` extracts Scala classes, objects, traits, functions, variables,
  imports, calls, and bounded `dead_code_root_kinds` metadata.
- `PreScan` returns deterministic names for import-map pre-scan.

Dead-code metadata lives in `dead_code_roots.go`. It only marks roots proven by
local syntax: `main` methods, objects extending `App`, traits and trait
methods, same-file trait implementations, overrides, Play controller actions,
Akka actor `receive` methods, lifecycle callbacks, JUnit methods, and
ScalaTest suite classes. Implicit/given resolution, macros, Play route files,
compiler plugins, dynamic dispatch, and broad public API surfaces remain query
exactness blockers rather than parser fallbacks.
