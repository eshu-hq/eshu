# Catalog OpenAPI Fragments

The OpenAPI 3.0 path documentation for Eshu's deployment-capability
surfaces — "what can this deployment do": capability maturity, component
extensions, query playbooks, and the surface inventory.

Layout:

- `capabilities.go` — `Capabilities`: `GET /api/v0/capabilities`.
- `component_extensions.go` — `ComponentExtensions`:
  `GET /api/v0/component-extensions` and
  `GET /api/v0/component-extensions/{component_id}/diagnostics`.
- `playbooks.go` — `Playbooks`: `GET /api/v0/query-playbooks` and
  `POST /api/v0/query-playbooks/resolve`.
- `surface_inventory.go` — `SurfaceInventory`:
  `GET /api/v0/surface-inventory`.

## Why its own package

These four routes all answer discovery questions about the deployment
itself rather than about repository or runtime content — a caller asks
"what can I call" or "what is this deployment's maturity" before asking
"what does the graph contain". Grouping them keeps that discovery-surface
boundary explicit and separate from the status/admin surface (`status/`).

## Adding a fragment

A new catalog route gets its own file exporting one JSON string constant,
matching the shape of the existing files. Register the constant in
`openapi/spec.go`'s concatenation and add the route to
[HTTP API Reference](../../../../../../docs/public/reference/http-api.md) in
the same PR. The assembled spec is a published wire contract: a fragment
added here but not wired into `spec.go` never reaches API consumers even
though the handler behind it still works.

## Move evidence

This tree is destination-only for the #6642 path split: these four
constants and their route sets were confirmed against the pre-move
`openapi_paths_*.go` files (`rg -o '"/api/v0[^"]*"'`) with no route added,
removed, or reworded.
