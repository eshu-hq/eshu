# Repository OpenAPI Paths

The OpenAPI 3.0 path fragments for the repository, package-registry, and
dependency routes.

Layout:

- `routes.go` — `Routes`: `/health`, `/api/v0/repositories`,
  `/api/v0/repositories/by-language`,
  `/api/v0/repositories/language-inventory`, `/api/v0/catalog`,
  `/api/v0/repositories/{repo_id}/context`,
  `/api/v0/repositories/{repo_id}/story`,
  `/api/v0/repositories/{repo_id}/tree`, and
  `/api/v0/repositories/{repo_id}/content`.
- `branches.go` — `Branches`: `GET /api/v0/repositories/{repo_id}/branches`.
- `freshness.go` — `Freshness`: `GET /api/v0/repositories/{repo_id}/freshness`.
- `stats_and_coverage.go` — `StatsAndCoverage`:
  `GET /api/v0/repositories/{repo_id}/stats` and
  `GET /api/v0/repositories/{repo_id}/coverage`.
- `dependencies.go` — `Dependencies`: `GET /api/v0/dependencies`.
- `package_registry.go` — `PackageRegistry`: `/api/v0/package-registry/packages`,
  `/api/v0/package-registry/versions`,
  `/api/v0/package-registry/dependencies`,
  `/api/v0/package-registry/correlations`, and
  `/api/v0/package-registry/dependency-chains`.
- `package_registry_aggregate.go` — `PackageRegistryAggregate`:
  `GET /api/v0/package-registry/packages/count` and
  `GET /api/v0/package-registry/packages/inventory`.

`routes.go` imports `openapi/schema` for the shared
`schema.EvidenceBoundaries` fragment embedded in the repository-context
route's response; every other file here is a self-contained JSON object
body with no import.

## Assembly

`openapi/spec.go` concatenates these constants — plus every other
`paths/<family>` leaf and the shared `openapi/schema` and `components`
blocks — into the single JSON document `Spec()` returns. `Routes`,
`StatsAndCoverage`, `Branches`, and `Freshness` are joined as one
contiguous group early in the document; `PackageRegistry`,
`PackageRegistryAggregate`, and `Dependencies` are joined later, next to
the `code` and `evidence` families. A dropped or misplaced constant here
silently changes the published wire contract at `/api/v0/openapi.json`,
and only `scripts/verify-openapi.sh`'s route cross-reference would catch
it.

## Related docs

- `docs/public/reference/http-api.md`
