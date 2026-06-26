# API Versioning and Migration

Eshu's HTTP API uses version prefixes to give clients a predictable upgrade
window. This page covers the version lifecycle, the deprecation headers
clients receive, and the migration path from `v0` to `v1`.

## What v0 and v1 Mean

- **`/api/v0/`** — the original API surface. Supported until the sunset date
  carried in every response header. After sunset, routes may be removed without
  further notice.
- **`/api/v1/`** — the alias for the current live surface. It resolves to the
  same handlers and same response shapes as v0. No new behavior, no divergence.

Both versions serve identical payloads. The version prefix selects the HTTP
contract, not the underlying feature set.

## Deprecation Headers

Every `/api/v0/` response carries two headers defined by [RFC 8594](https://datatracker.ietf.org/doc/html/rfc8594):

| Header | Value | Meaning |
|--------|-------|---------|
| `Deprecation` | `true` | The endpoint is deprecated and will be removed. Clients should migrate. |
| `Sunset` | `<HTTP-date>` | The date after which the endpoint may become unavailable. |

The sunset date is 12 months from the date the deprecation headers were
introduced. The exact date is available in the environment variable registry
(`ESHU_API_V0_SUNSET_DATE`) so operators can keep it accurate without
redeploying.

`/api/v1/` responses carry neither header.

## Migration Steps

1. Replace every `/api/v0/` prefix with `/api/v1/` in client code, scripts,
   CI/CD pipelines, and OpenAPI client generators.
2. For OpenAPI consumers: regenerate from `GET /api/v1/openapi.json` instead
   of `/api/v0/openapi.json`. The spec declares both versions.
3. Drop the `Deprecation` and `Sunset` header handling logic after switching;
   v1 responses carry no such headers.
4. Keep bearer tokens, auth flows, shared wire contracts (`Accept:
   application/eshu.envelope+json`), and error-code handling unchanged. Only
   the URL prefix changes.

No request body, response body, or status-code changes are required.

## Rollback Policy

If a client breakage is discovered during migration, revert the URL prefix
back to `/api/v0/`. v0 and v1 serve the same handlers and return identical payloads
while both are active, so rollback is a one-line URL change.

Do not ship a rolled-back client into production past the sunset date.

## Timeline

| Milestone | Date | Notes |
|-----------|------|-------|
| v1 alias introduced | V-1 merge date | `/api/v1/` resolves to the same handlers as `/api/v0/`. |
| Deprecation announced | V-2 merge date | `Deprecation` and `Sunset` headers appear on every `/api/v0/` response. |
| Sunset date | 12 months after V-2 merge | `/api/v0/` may be removed after this date. Exact date set in `ESHU_API_V0_SUNSET_DATE`. |

Operators may adjust the sunset date via the environment variable. The header
value always reflects the configured variable, not a hardcoded string.

## Related References

- [HTTP API Reference](http-api.md)
- [RFC 8594 — The Sunset HTTP Header Field](https://datatracker.ietf.org/doc/html/rfc8594)
- [Environment Variable Registry](env-registry.md)
