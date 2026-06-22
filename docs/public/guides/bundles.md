# Bundles

Bundles are read-only catalog candidates for pre-indexed dependency or library
graph content.

A bundle record is a package registry identity (a `:Package` node materialized
by the reducer from registry and SBOM facts) that was already indexed and can be
searched from the active query backend. The current Go platform does not ship a
public HTTP upload or import route for `.eshu` archives.

Normal repository indexing excludes vendored and dependency directories such as
`vendor/` and `node_modules/` by default. Use bundle search when you want to
find existing pre-indexed package registry candidates instead of walking
vendored source in every repository.

## Search Bundles

The query surface exposes a searchable view over the package registry bundle
catalog. The `query` is matched case-insensitively against each package's
normalized name, namespace, or PURL, and an optional `ecosystem` scopes the read
to one package ecosystem:

```bash
curl -s \
  -X POST http://localhost:8080/api/v0/code/bundles \
  -H 'Content-Type: application/json' \
  -d '{"query":"react","ecosystem":"npm","limit":10}'
```

Each result reports `package_id`, `name`, `ecosystem`, `registry`, `namespace`,
`purl`, and `version_count`.

If you use MCP, the matching tool is `search_registry_bundles`.

## Where bundles help

- onboarding environments that need dependency internals preloaded
- shared test fixtures for large library graphs
- finding dependency source candidates without indexing vendored trees in every
  repository
- service environments where pre-indexed dependency graph content is easier to
  query than a fresh dependency scan

## Current shipped surface

The shipped Go platform documents and tests these bundle surfaces:

- `POST /api/v0/code/bundles`
- MCP `search_registry_bundles`

Public docs should not assume bundle upload routes or CLI wrappers beyond those
shipped paths.

## Next steps

- [HTTP API](../reference/http-api.md) — bundle import and query routes
- [Visualization](visualization.md) — read-only query visualization in Neo4j
- [Start Here](../start-here.md) — choose the right first-run path
