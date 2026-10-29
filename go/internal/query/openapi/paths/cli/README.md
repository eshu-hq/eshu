# CLI OpenAPI Fragments

The OpenAPI 3.0 path documentation for Eshu's CLI-support endpoints: the
pure-function routes the extracted thin CLI calls instead of linking the
rendering, composition, capture, validation, and scoring logic.

Layout:

- `render.go` — `Render`: `POST /api/v0/investigation/packet/render`.
- `compose.go` — `Compose`: `POST /api/v0/service/report/compose`.
- `capture.go` — `Capture`: `POST /api/v0/report/bundle/capture`.
- `validate.go` — `Validate`: `POST /api/v0/report/bundle/validate`.
- `score.go` — `Score`: `POST /api/v0/answer-quality/score`.

## Why its own package

The thin-CLI surface is a narrow, self-contained family with no fields in
common with the other families: every route takes an already-fetched artifact
in the request body and returns a rendering, composition, bundle, verdict, or
score without touching a store. Splitting it out of the flat
`openapi_paths_*.go` layout gives it a package boundary sized to match, and
leaves room to grow independently if the CLI surface expands later without
disturbing an unrelated family.

## Adding a fragment

A new CLI route gets its own file exporting one JSON string constant,
matching the shape of the existing files. Register the constant in
`openapi/spec.go`'s concatenation and add the route to
[HTTP API Reference](../../../../../../docs/public/reference/http-api.md) in
the same PR. The assembled spec is a published wire contract: a fragment
added here but not wired into `spec.go` never reaches API consumers even
though the handler behind it still works.
