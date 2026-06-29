# Scala Parser

This package owns Scala language extraction that can run without importing the
parent `internal/parser` package. The parent engine still owns registry
dispatch and tree-sitter runtime setup.

Exports:

- `Parse` extracts Scala classes, objects, traits, functions, variables,
  imports, calls, exact literal Play/http4s route entries, and bounded
  `dead_code_root_kinds` metadata.
- `PreScan` returns deterministic names for import-map pre-scan.

Dead-code metadata lives in `dead_code_roots.go`. It only marks roots proven by
local syntax: `main` methods, objects extending `App`, traits and trait
methods, same-file trait implementations, overrides, Play controller actions,
Akka actor `receive` methods, lifecycle callbacks, JUnit methods, and
ScalaTest suite classes. Route metadata lives in `framework_routes.go`. Play
route files use a bounded route-file parser for `conf/routes` and `.routes`
files; Scala source uses tree-sitter for literal http4s `HttpRoutes.of` cases.
Dynamic Play routes, namespaced controller targets, generated route files,
broader http4s extractor shapes, implicit/given resolution, macros, compiler
plugins, dynamic dispatch, and broad public API surfaces remain query exactness
blockers rather than parser fallbacks.
