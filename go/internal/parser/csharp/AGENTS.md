# C# Parser Agent Notes

Read `language.go` first for payload flow, then `dead_code_roots.go` for
C# reachability evidence. Keep this package parent-independent: use
`internal/parser/shared` for payload, source, sorting, and tree-sitter node
helpers. Do not import `internal/parser`. The one exception is the external
`csharp_test` package (`csharp_cfg_dataflow_test.go`,
`csharp_dead_code_roots_test.go`, `csharp_route_semantics_test.go`,
`engine_csharp_taint_test.go`, `csharp_test_helpers_test.go`, relocated from
the parent by #6062): it compiles separately from `internal/parser`, so it may
import the parent engine and `internal/parser/parsertest` for black-box
coverage of `parser.DefaultEngine().ParsePath`. Production code and the
in-package `package csharp` tests never import the parent.

Preserve existing payload keys and sorting unless a parser contract change is
covered by tests and downstream materialization updates.

C# dead-code roots are parser evidence, not a full semantic model. Keep roots
bounded to syntax this package sees directly: local interface declarations,
same-file base lists, method attributes, explicit overrides, ASP.NET controller
shape, and hosted-service base types.

The value-flow/taint subsystem (`dataflow_*.go`) is opt-in behind
`Options.EmitDataflow` and must stay byte-identical when off. Hard rules:

- Every source and sink MUST be corroborated by both the framework attribute or
  receiver type AND a matching `using` directive. Never match a source/sink on a
  name alone. The same-name-no-match guard is the load-bearing acceptance test
  (`TestCSharpTaintIgnoresSameNamedLocalSourceAndSink`).
- Sink receiver-type inference is local and explicit-typed only
  (`csharpBuildTypeEnv`). Do not "guess" a receiver's type for an
  implicit/`var` local; an unknown type MUST NOT match a sink. Preferring a
  missed finding over a fabricated one is the honesty contract.
- When extending the catalog, bump nothing manually: the content version is a
  hash of the catalog (`csharpTaintCatalogVersion`); just edit the specs.
- If you add/rename buckets or change the catalog, update
  `docs/public/reference/value-flow-emission.md`, the capability matrix/overlay
  specs, and regenerate the catalog artifact in the same change.
