# CI/CD OpenAPI Fragments

The OpenAPI 3.0 path documentation for Eshu's CI/CD run-correlation reads.
The smallest leaf in the `openapi/paths` tree.

Layout:

- `routes.go` — `Routes`: `GET /api/v0/ci-cd/run-correlations`.
- `run_correlation_aggregate.go` — `RunCorrelationAggregate`:
  `GET /api/v0/ci-cd/run-correlations/count` and
  `GET /api/v0/ci-cd/run-correlations/inventory`.

## Why its own package

Run-correlation is a narrow, self-contained read surface with no fields in
common with the other families; splitting it out of the flat
`openapi_paths_*.go` layout gives it a package boundary sized to match, and
leaves room to grow independently if the CI/CD surface expands later
without disturbing an unrelated family.

## Adding a fragment

A new CI/CD route gets its own file exporting one JSON string constant,
matching the shape of the existing files. Register the constant in
`openapi/spec.go`'s concatenation and add the route to
[HTTP API Reference](../../../../../../docs/public/reference/http-api.md) in
the same PR. The assembled spec is a published wire contract: a fragment
added here but not wired into `spec.go` never reaches API consumers even
though the handler behind it still works.

## Move evidence

This tree is destination-only for the #6642 path split: these two
constants and their route sets were confirmed against the pre-move
`openapi_paths_*.go` files (`rg -o '"/api/v0[^"]*"'`) with no route added,
removed, or reworded.
