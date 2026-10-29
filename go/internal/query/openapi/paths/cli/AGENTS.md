# CLI OpenAPI Fragments — Agent Instructions

Scope: `go/internal/query/openapi/paths/cli/` (package `cli`).

## Ownership

This leaf owns the OpenAPI 3.0 path documentation for the CLI-support
surface: `render.go` (`Render`), `compose.go` (`Compose`),
`capture.go` (`Capture`), `validate.go` (`Validate`), and `score.go`
(`Score`). Each file is a JSON string constant only — no handler logic, no
HTTP wiring.

- This package imports nothing from the rest of the repo. It MUST NOT
  import the parent `openapi` package or the `query` root — both import
  this package, so either direction is a cycle.
- A constant here is inert until `openapi/spec.go` concatenates it into the
  published spec. Adding, renaming, or removing an exported constant here
  without updating `spec.go` in the same change silently changes the
  published OpenAPI document without changing what the server actually
  serves, or vice versa.
