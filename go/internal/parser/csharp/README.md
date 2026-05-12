# C# Parser

This package owns C# language extraction that can run without importing the
parent `internal/parser` package. The parent engine still owns registry
dispatch and tree-sitter runtime setup.

`language.go` owns payload assembly. `dead_code_roots.go` owns the bounded
root model used by dead-code analysis. `dead_code_syntax.go` keeps syntax
helpers for attributes, modifiers, type names, base lists, and C# `Main`
signatures. Same-file interface evidence records method arity and qualified
type context so overloaded methods and duplicate class names do not become
roots by name alone.

Exports:

- `Parse` extracts C# declarations, imports, calls, inheritance metadata, and
  common parser payload fields. It also emits bounded dead-code root metadata
  for C# entrypoints, constructors, overrides, interface methods, ASP.NET
  controller actions, hosted-service callbacks, test methods, and serialization
  callbacks.
- `PreScan` returns deterministic names for import-map pre-scan.
