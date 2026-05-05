# Bundles

Bundles are the explicit import path for prebuilt `.eshu` graph snapshots in the
current Go platform.

A `.eshu` bundle is a portable file containing pre-indexed graph data and
metadata. Import a bundle when you want dependency or library internals without
walking vendored source trees as part of the normal repository scan.

Normal repository indexing excludes vendored and dependency directories such as
`vendor/` and `node_modules/` by default. Bundles are how you opt into that
deeper graph content on purpose.

## Import a bundle

Use the HTTP API to import a prebuilt `.eshu` file into the active graph
database:

```bash
curl -s \
  -X POST http://localhost:8080/api/v0/bundles/import \
  -F bundle=@vendor-lib.eshu \
  -F clear_existing=true
```

The import route accepts:

- `multipart/form-data`
- file field `bundle`
- optional `clear_existing=true|false`

## Search the bundle catalog

The query surface also exposes a searchable bundle catalog view:

```bash
curl -s \
  -X POST http://localhost:8080/api/v0/code/bundles \
  -H 'Content-Type: application/json' \
  -d '{"query":"react","unique_only":true}'
```

If you use MCP, the matching tool is `search_registry_bundles`.

## Where bundles help

- onboarding environments that need dependency internals preloaded
- shared test fixtures for large library graphs
- controlled imports of dependency source without indexing vendored trees in
  every repository
- service environments where a prebuilt bundle artifact is easier to ship than
  a fresh dependency scan

## Current shipped surface

The shipped Go platform documents and tests these bundle surfaces:

- `POST /api/v0/bundles/import`
- `POST /api/v0/code/bundles`
- MCP `search_registry_bundles`

Public docs should not assume extra CLI wrappers beyond those shipped paths.

## Next steps

- [HTTP API](../reference/http-api.md) — bundle import and query routes
- [Visualization](visualization.md) — read-only query visualization in Neo4j
- [Quickstart](../getting-started/quickstart.md) — index your first repo from scratch
