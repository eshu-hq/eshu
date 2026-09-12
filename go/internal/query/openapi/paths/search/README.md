# Search OpenAPI Fragments

The OpenAPI 3.0 path documentation for Eshu's read/retrieval surface:
content search, natural-language ask, entity resolution, graph entities,
semantic search, and semantic evidence.

Layout:

- `content.go` — `Content`: `POST /api/v0/content/entities/read`,
  `POST /api/v0/content/entities/search`, `POST /api/v0/content/files/lines`,
  `POST /api/v0/content/files/read`, and `POST /api/v0/content/files/search`.
- `ask.go` — `Ask`: `POST /api/v0/ask`.
- `entities.go` — `Entities`: `POST /api/v0/entities/resolve`,
  `GET /api/v0/entities/{entity_id}/context`,
  `GET /api/v0/services/{service_name}/context`,
  `GET /api/v0/services/{service_name}/story`,
  `GET /api/v0/workloads/{workload_id}/context`, and
  `GET /api/v0/workloads/{workload_id}/story`. Imports `openapi/schema` for
  `schema.EvidenceBoundaries`.
- `graph_entities.go` — `GraphEntities`: `GET /api/v0/graph/entities`.
- `semantic.go` — `Semantic`: `POST /api/v0/search/semantic`.
- `semantic_evidence.go` — `SemanticEvidence`:
  `GET /api/v0/semantic/code-hints` and
  `GET /api/v0/semantic/documentation-observations`.

## Why its own package

The retrieval surface is the busiest family in the OpenAPI tree by route
count. Splitting it out of the flat `openapi_paths_*.go` layout gives each
read shape (content, ask, entity, graph, semantic) its own file and keeps
the family's tests and reviewers scoped to one directory instead of one
package shared with every other family.

## Adding a fragment

A new search route gets its own file exporting one JSON string constant,
matching the shape of the existing files. Register the constant in
`openapi/spec.go`'s concatenation and add the route to
[HTTP API Reference](../../../../../../docs/public/reference/http-api.md) in
the same PR. The assembled spec is a published wire contract: a fragment
added here but not wired into `spec.go` never reaches API consumers even
though the handler behind it still works.

## Move evidence

This tree is destination-only for the #6642 path split: these six
constants and their route sets were confirmed against the pre-move
`openapi_paths_*.go` files (`rg -o '"/api/v0[^"]*"'`) with no route added,
removed, or reworded.
