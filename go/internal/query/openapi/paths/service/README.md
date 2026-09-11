# Service OpenAPI Paths

The OpenAPI 3.0 path fragments for the service-catalog and per-service
intelligence-report routes.

Layout:

- `catalog.go` — `Catalog`: `GET /api/v0/service-catalog/correlations`.
- `intelligence_report.go` — `IntelligenceReport`:
  `GET /api/v0/services/{service_name}/intelligence-report`.

Both were already exported path constants before the #6060 move; this
package is their first home with a matching directory name, not a new
surface. Neither imports `openapi/schema`.

## Assembly

`openapi/spec.go` concatenates `service.IntelligenceReport` early, next to
the `code` family, and `service.Catalog` much later, next to
`infrastructure.Kubernetes` — the two are not joined together. A dropped
or misplaced constant here silently changes the published wire contract at
`/api/v0/openapi.json`, and only `scripts/verify-openapi.sh`'s route
cross-reference would catch it.

## Related docs

- `docs/public/reference/http-api.md`
