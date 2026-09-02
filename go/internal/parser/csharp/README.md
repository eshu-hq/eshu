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

`framework_routes.go` owns exact ASP.NET route entries. It emits parser
payload only for literal ASP.NET Core MVC/Web API attributes and literal
minimal API `Map*` calls with named handlers. Convention routes, token
substitution, lambdas, generated routes, implicit global usings, and runtime
endpoint discovery stay outside the contract.

The `dataflow_*.go` files own the opt-in value-flow/taint subsystem, gated on
`Options.EmitDataflow`:

- `dataflow_taint.go` holds the source/sink catalog and its content version.
  Sources are ASP.NET Core model-binding parameters verified by a
  `Microsoft.AspNetCore.Mvc` using; sinks are ADO.NET `SqlCommand` execution
  verified by a `System.Data.SqlClient`/`Microsoft.Data.SqlClient` using and
  `Process.Start` verified by a `System.Diagnostics` using. The attribute/using
  corroboration is what rejects a same-named local class or attribute as a
  false positive.
- `dataflow_lower.go` lowers a method/constructor/local-function body into a
  control-flow graph.
- `dataflow_bindings.go` holds parameter/attribute extraction, the per-line
  def/use index, and the per-function type environment used for sink receiver
  inference (explicit-typed parameters and locals only; `var` locals are
  intentionally omitted).
- `dataflow_summary.go` derives durable interprocedural summaries and source
  ports (emitted only when a repository identity and namespace are present).
- `dataflow_emit.go` is the gate: it attaches the buckets only when
  `Options.EmitDataflow` is set, leaving a default parse byte-identical.

## Taint coverage

- **Sources (kind `http_request`):** `[FromQuery]`, `[FromBody]`, `[FromRoute]`,
  `[FromForm]` action parameters, with a `Microsoft.AspNetCore.Mvc` using.
- **Sinks (kind `sql`):** `SqlCommand.ExecuteReader` / `ExecuteNonQuery` /
  `ExecuteScalar`, with a `System.Data.SqlClient` or `Microsoft.Data.SqlClient`
  using. **Sink (kind `command_injection`):** `Process.Start`, with a
  `System.Diagnostics` using.
- **Sanitizers:** none in v1 (documented gap; parameterized-query helpers are a
  follow-up).
- **Known unsupported (honesty contract):** sinks whose receiver is a
  `var`/implicit-typed local with no inferable declared type are not matched;
  cross-file/cross-package composition is the reducer's job; the matrix cannot
  express the `ESHU_EMIT_DATAFLOW` gate, so the capability is registered as
  gated.

## Engine test surface

`csharp_cfg_dataflow_test.go`, `csharp_dead_code_roots_test.go`,
`csharp_route_semantics_test.go`, and `engine_csharp_taint_test.go` (external
package `csharp_test`, relocated from the parent by #6062) drive
`parser.DefaultEngine().ParsePath` against inline and `tests/fixtures/deadcode`
C# fixtures: 19 test functions, from
`rg --no-filename -o '^func Test' csharp_cfg_dataflow_test.go csharp_dead_code_roots_test.go csharp_route_semantics_test.go engine_csharp_taint_test.go | wc -l`.
They may import `internal/parser` because they sit in the external
`csharp_test` package: an external test package is compiled separately from
`internal/parser` itself, so the import does not close the cycle that
`parsertest` would otherwise create. Keep that exception limited to black-box
tests of the public parent engine. Shared assertions come from
`go/internal/parser/parsertest`; `csharp_test_helpers_test.go` holds only
`csharpFixturePath` (the external tests sit one level below `internal/parser`,
out of reach of the parent's fixture helper) and `writeCSharpTestFile` (creates
parent directories before delegating to `parsertest.WriteFile` for the nested
`Controllers/` fixture). The 1 `package csharp` test file
(`equivalence_dump_test.go`, the guarded `CS_PARSE_DUMP` differential) stays
white-box and must not import the parent.

Run the engine tests from the `go/` module root with
`go test ./internal/parser/csharp -count=1`; pin a subset with
`../scripts/go-test-run-guard.sh 4 TestDefaultEngineParsePathCSharp -- ./internal/parser/csharp -count=1`,
which runs from the `go/` module root and fails closed if fewer than 4 tests
match (minimum derived from
`go test -list 'TestDefaultEngineParsePathCSharp' ./internal/parser/csharp`).

Exports:

- `Parse` extracts C# declarations, imports, calls, inheritance metadata, and
  common parser payload fields. It also emits bounded dead-code root metadata
  for C# entrypoints, constructors, overrides, interface methods, ASP.NET
  controller actions, hosted-service callbacks, test methods, and serialization
  callbacks. It emits exact ASP.NET route entries only for local source-proven
  literal routes and named handlers; those entries feed existing endpoint and
  `HANDLES_ROUTE` projection. When `Options.EmitDataflow` is set it
  additionally emits the value-flow/taint buckets described above.
- `PreScan` returns deterministic names for import-map pre-scan.
