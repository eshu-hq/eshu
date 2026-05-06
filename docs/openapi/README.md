# OpenAPI

This directory holds checked-in OpenAPI contracts that are not generated from
the public query router.

`runtime-admin-v1.yaml` describes the service-local `/admin/*`, `/healthz`,
`/readyz`, and `/metrics` surface used by long-running runtimes. The public
`/api/v0` OpenAPI document is assembled in Go under `go/internal/query` and
served at `/api/v0/openapi.json`. The same router serves public browser docs at
`/api/v0/docs` and `/api/v0/redoc`.
