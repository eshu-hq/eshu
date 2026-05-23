# AGENTS.md - internal/parser/rust

## Read First

1. `README.md` and `doc.go`.
2. `parser.go`, `helpers.go`, `metadata.go`, `where.go`,
   `nested_attributes.go`, and `path_attributes.go`.
3. `module_resolution.go`, `macro_declarations.go`, and `cargo_cfg.go`.
4. `parser_test.go`, `metadata_test.go`, `module_resolution_test.go`,
   `cargo_cfg_test.go`, and `root_metadata_test.go`.

## Guardrails

- MUST NOT import `internal/parser`; parent wrappers own registry
  dispatch, runtime lookup, path normalization, and Engine signatures.
- MUST let callers own tree-sitter parser construction and closing.
- MUST preserve parent payload shape and deterministic `Parse`/`PreScan`
  ordering.
- MUST keep root metadata conservative. Main, test, Tokio, public API, and benchmark
  roots require direct function names, exact `pub`, Cargo entrypoint paths,
  file-local Criterion targets, or direct attributes.
- MUST limit structured Rust metadata to direct syntax: brace imports, module
  declarations, derives, cfg attributes, item attributes, generic parameters,
  where predicates, associated-type constraints, and higher-ranked trait-bound
  predicates.
- MUST NOT infer arbitrary macro expansion, feature solving, cfg evaluation,
  cross-crate semantics, or filesystem-backed module resolution.
- MUST keep field and enum-variant attributes on owned annotation rows.
- MUST keep impl target metadata bounded to the receiver type; `where` clauses
  are evidence, not part of `target`.
- MUST keep exactness blockers honest: unresolved cfg rows keep
  `cfg_unresolved`, and macro-origin module/import rows keep
  `macro_expansion_unavailable` until a downstream resolver and tests exist.

## Change Scope

- Start Rust behavior changes with focused parent parser tests or child-package
  tests.
- Module path metadata stays file-local. `declared_path_candidates` names
  current-file-relative candidates unless explicit `#[path = "..."]` replaces
  them.
- Do not change payload keys without downstream parser tests, fixture updates,
  and architecture-owner approval.
