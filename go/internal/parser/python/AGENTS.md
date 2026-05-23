# AGENTS.md - internal/parser/python

The Python adapter owns `.py` and notebook code-cell evidence. Use `README.md`
and `doc.go` for the current package contract.

## Read First

1. `README.md` and `doc.go`.
2. `language.go`, `shared_helpers.go`, `notebook.go`, and
   `notebook_temp.go`.
3. `dead_code_roots.go`, `lambda_roots.go`, `public_api_roots.go`,
   `semantics.go`, `imports.go`, `call_inference.go`,
   `annotation_support.go`, and `generator_support.go`.
4. `notebook_test.go`, `language_test.go`, and parent `engine_python_*` tests.

## Mandatory Guardrails

- This package MUST NOT import `internal/parser`; parent wrappers own registry,
  runtime, path normalization, and Engine signatures.
- `Parse` receives a caller-owned tree-sitter parser and must not close it.
- `NotebookSource` indexes executable code cells only. Markdown, raw, blank, or
  malformed cells must not become Python source; invalid notebook JSON returns
  an error instead of partial source.
- SAM and serverless roots only mark handlers when the config declares a Python
  runtime and the handler resolves inside the repository.
- Public API, dunder protocol, property, Lambda, route, ORM, and receiver
  evidence must stay source-backed and bounded.
- Generic parser helpers belong in `internal/parser/shared` when more than one
  language needs them.

## Change Scope

- Parse behavior starts with `language_test.go` or parent `engine_python_*`
  tests.
- Notebook behavior starts with `notebook_test.go`.
- Lambda config discovery must remain bounded from the source directory to the
  repository root.
- Do not change payload bucket names or wire fields without downstream query,
  fixture, and docs updates plus architecture-owner approval.
