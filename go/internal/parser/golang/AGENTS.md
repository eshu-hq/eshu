# AGENTS.md - internal/parser/golang

The Go adapter owns Go syntax evidence only. The README and `doc.go` hold the
package contract; use this file for local guardrails before changing parser
behavior.

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

## Mandatory Guardrails

- This package MUST NOT import `internal/parser`; parent wrappers own registry,
  runtime, path normalization, and option aggregation.
- `Parse` and `PreScan` must preserve deterministic bucket names, fields, and
  ordering. Sort payload rows and pre-scan results before returning.
- Dead-code roots require bounded Go source evidence: syntax, registration,
  same-file proof, same-package proof, scoped receiver proof, or qualified
  package contract evidence. Name-only method matches are not enough.
- `ImportedInterfaceParamMethods` stays file-local; package grouping belongs in
  the parent `Engine` wrapper.
- Embedded SQL line numbers must refer to the original Go source.
- Do not reintroduce per-call full-tree walks for parent or variable lookup.
  Use the existing parent, variable-type, and imported-variable-type indices so
  per-file cost stays linear.
- This package emits no telemetry directly; parent runtime and collector paths
  own parse timing and failures.

## Change Scope

- New Go payload fields or root kinds need failing parser tests first, then the
  narrow helper that owns the evidence.
- SQL extraction changes start in `embedded_sql_test.go`.
- Same-package interface evidence changes must test both the child helper and
  the parent package pre-scan wrapper.
- Do not change `.go` registry ownership, `embedded_sql_queries`,
  `dead_code_root_kinds`, or `function_calls` without downstream facts,
  content shape, reducer/query impact review, docs, and architecture-owner
  approval.
