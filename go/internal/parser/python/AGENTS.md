# AGENTS.md - internal/parser/python

## Read First

1. `README.md` and `doc.go`.
2. `language.go`, `shared_helpers.go`, `notebook.go`, and
   `notebook_temp.go`.
3. `dead_code_roots.go`, `lambda_roots.go`, `public_api_roots.go`,
   `semantics.go`, `imports.go`, `call_inference.go`,
   `annotation_support.go`, and `generator_support.go`.
4. `notebook_test.go`, `language_test.go`, and parent `engine_python_*` tests.

## Guardrails

- MUST NOT import `internal/parser`; parent wrappers own registry,
  runtime, path normalization, and Engine signatures.
- MUST let callers own and close the tree-sitter parser passed to `Parse`.
- MUST make `NotebookSource` index executable code cells only. Markdown, raw, blank, or
  malformed cells must not become Python source; invalid notebook JSON returns
  an error instead of partial source.
- MUST mark SAM and serverless roots only when the config declares a Python
  runtime and the handler resolves inside the repository.
- MUST keep public API, dunder protocol, property, Lambda, route, ORM, and
  receiver evidence source-backed and bounded.
- MUST move generic parser helpers to `internal/parser/shared` only when more than one
  language needs them.

## Change Scope

- Start parse behavior with `language_test.go` or parent `engine_python_*`
  tests.
- Start notebook behavior with `notebook_test.go`.
- Keep Lambda config discovery bounded from the source directory to the
  repository root.
- Do not change payload bucket names or wire fields without downstream query,
  fixture, and docs updates plus architecture-owner approval.
