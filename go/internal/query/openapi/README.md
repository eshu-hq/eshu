# OpenAPI Assembly

Assembles and serves the published OpenAPI 3.0 document for the Eshu Query
API: `Spec` concatenates the document prefix, every route family's path
fragments, and the shared components block, then substitutes the running
build's version into the `__ESHU_VERSION__` placeholder. `ServeSpec` writes
the assembled document as JSON; `ServeSwaggerUI` and `ServeReDoc` serve the
two browser viewers over embedded static HTML.

Layout:

- `spec.go` — `Spec`, `ServeSpec`, `ServeSwaggerUI`, `ServeReDoc`, and the
  embedded Swagger UI / ReDoc HTML shells.
- `prefix.go` — `specPrefix`: the document header (`info`, `servers`,
  `tags`) that opens the JSON document before the first path fragment.
- `components.go` plus `components_local_identity.go`,
  `components_provider_configs.go`, `components_replatforming.go`,
  `components_responses.go`, `components_sign_in_policy.go`, and
  `components_workload_session.go` — the shared `components.schemas` and
  `components.parameters` block, split by subject because one file holding
  all of it would be unreviewable. `components.go` holds the parameters and
  the bulk of the schemas and concatenates the other six in place.

The route fragments live one package per family under `paths/` (`paths/auth`,
`paths/code`, `paths/supplychain`, and the rest); `spec.go` imports each and
concatenates its exported constants in the exact order the published `paths`
block must render them. Reordering that concatenation reorders the published
document, not just this package's source — the JSON key order it produces is
part of what a client-facing diff of the spec shows.

## Move evidence

This package, its six `components_*.go` files, and `prefix.go` moved here
verbatim from the query root (Issue #6060 lane C, #6642):
`openapi.go` -> `spec.go`, `openapi_prefix.go` -> `prefix.go`,
`openapi_components.go` -> `components.go`, `openapi_components_auth.go` ->
`components_local_identity.go`, and the remaining
`openapi_components_*.go` files kept their subject names. Only the package
clause, the `paths/<family>` and `openapi/schema` import paths, and (for the
one renamed file) the file name changed — the string content each
constant assembles is unchanged. The query root keeps `OpenAPISpec`,
`ServeOpenAPI`, `ServeSwaggerUI`, and `ServeReDoc` as thin forwards in
`handler.go` so existing callers (`cmd/api`, `cmd/mcp-server`) needed no
changes.

Verification for this move: `go build ./internal/query/...` and
`gofmt -l internal/query/openapi` from `go/` must be clean, and
`scripts/verify-openapi.sh` must stay green — see `AGENTS.md` in this
directory for what that script checks.
