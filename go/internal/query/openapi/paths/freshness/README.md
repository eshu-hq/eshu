# Freshness OpenAPI Fragments

The OpenAPI 3.0 path documentation for Eshu's freshness and change-tracking
reads: freshness causality, repo and service changed-since, and generation
tracking.

Layout:

- `causality.go` — `Causality`: `GET /api/v0/status/freshness-causality`.
- `changed_since.go` — `ChangedSince`:
  `GET /api/v0/freshness/changed-since`.
- `generations.go` — `Generations`: `GET /api/v0/freshness/generations`.
- `service_changed_since.go` — `ServiceChangedSince`:
  `GET /api/v0/freshness/services/changed-since`.

## Why its own package

Freshness is a distinct read concern from the general status and admin
surfaces (`status/`) and from documentation/evidence freshness
(`evidence/routes.go`'s evidence-packet freshness read stays with its
packet, not here): these four routes answer "what changed and when" over
repos, services, and generations specifically, and reviewing them together
keeps that boundary visible.

## Adding a fragment

A new freshness route gets its own file exporting one JSON string constant,
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
