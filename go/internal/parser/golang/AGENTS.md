# AGENTS.md - internal/parser/golang

## Read First

1. `README.md` and `doc.go`.
2. `language.go`, `prescan.go`, `call_chain_metadata.go`, and `embedded_sql.go`.
3. `dead_code_roots.go`, `dead_code_registrations.go`,
   `dead_code_semantic_roots.go`, `dead_code_semantic_helpers.go`,
   `dead_code_semantic_flows.go`, and `function_literal_reachability.go`.
4. `package_interface_prescan.go`, `parent_lookup.go`,
   `variable_type_index.go`, and `imported_variable_type_index.go`.
5. Parent tests in `go/internal/parser/go*_test.go` before changing emitted
   payload shape.

## Guardrails

- MUST NOT import `internal/parser`; parent wrappers own registry,
  runtime, path normalization, and option aggregation.
- MUST preserve deterministic bucket names, fields, ordering, and pre-scan
  results.
- MUST keep direct method roots scoped to receiver evidence; same-method-name
  fallback is not enough.
- MUST treat function-value references as reachability evidence only when source
  text proves escape through a bounded call, field, return, callback, or
  interface contract.
- MUST bound imported package evidence to qualified package contracts and
  scoped imported-variable receiver types.
- MUST keep embedded SQL line numbers tied to the original Go source.
- MUST keep parent, variable-type, and imported-variable-type indices in use;
  do not reintroduce per-call full-tree walks.
- MUST keep branch counting and cyclomatic complexity consistent with the
  parent parser contract.
- MUST emit no telemetry directly; parent runtime and collector paths own parse
  timing and failures.

## Change Scope

- Add Go payload fields or root kinds with failing parser tests first, then the
  narrow helper that owns the evidence.
- Start SQL extraction changes in `embedded_sql_test.go`.
- Test same-package interface evidence in both the child helper and
  the parent package pre-scan wrapper.
- Do not change `.go` registry ownership, `embedded_sql_queries`,
  `dead_code_root_kinds`, or `function_calls` without downstream facts,
  content shape, reducer/query impact review, docs, and architecture-owner
  approval.
