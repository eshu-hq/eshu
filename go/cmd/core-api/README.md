# core-api

## Purpose

`eshu-core-api` is the Wave-6 core serving binary (step-3 gating ruling
D1). It serves the full forwarded query surface from Postgres and the
pinned Neo4j backend directly, so the satellite can point `--core-base-url`
at it. No IaC YAML may reference a core Service until this binary lands
(ruling D4).

## Ownership boundary

This binary owns flags, store wiring, and `ListenAndServe` only. Route
registration, the auth middleware chain, and secret-free handler wiring live
in `go/internal/query/serve.go` (`NewCoreServeMux`); the secret-sealed auth
adapters live here in package main because `go/internal/query` must never
import secretcrypto (`TestNoQueryFileImportsSecretcrypto`). The served
method set is frozen in
`go/internal/query/testdata/core_served_methods_frozen.txt` with a
three-leg test (`core_served_methods_test.go`).

## Entry points

- `main` and `run` in `go/cmd/core-api/main.go`
- `eshu-core-api --version` prints the build-time version and exits before
  opening backends
- `eshu-core-api --addr host:port` (default `:8080`)

## Configuration

All operator input arrives through the environment (flags carry only the
listen address):

- `ESHU_QUERY_PROFILE` (default `production`)
- `ESHU_GRAPH_BACKEND` (must be `neo4j`; anything else fails startup)
- Postgres: `ESHU_FACT_STORE_DSN` / `ESHU_CONTENT_STORE_DSN` /
  `ESHU_POSTGRES_DSN` via `runtime.OpenPostgres`
- Neo4j: `ESHU_NEO4J_URI` / `NEO4J_URI`, username, password, database via
  `runtime.OpenNeo4jDriver`
- `ESHU_API_KEY` (shared key; headerless reads stay open only when no auth
  source is configured)
- Auth: `ESHU_SCOPED_TOKENS_FILE`, `ESHU_AUTH_RESOURCE_URI`, OIDC/GitHub
  config files, `ESHU_SAML_PROVIDERS_JSON`
- `ESHU_AUTH_SECRET_ENC_KEY(_FILE)` (DEK; missing fails secret writes
  closed, never silently unencrypted)
- Setup: `ESHU_AUTH_BOOTSTRAP_MODE`, `ESHU_ADMIN_USERNAME`,
  `ESHU_ADMIN_PASSWORD(_FILE)`

## Probes

- `GET /health` — shallow process check (`{"status":"ok"}`), public.
- `GET /readyz` — backend-gated: 200 only when both Postgres and Neo4j
  answer, else the permanent 503 with no `Retry-After`, public.

## Telemetry

No-Observability-Change: the binary reuses the shared `eshu_dp_*`
instruments (`telemetry.NewInstruments`) and the process logger; it adds no
new metric, span, or log key. The Prometheus handler serves on `/metrics`
(public, behind the same auth chain).

## No-regression proof (Wave-6 review findings)

No-Regression Evidence: `TestCoreOuterMiddlewareV1CarriesNoDeprecation`
(red before the header/rewrite reorder, green after) plus the full
`go/internal/query` and `go/cmd/core-api` suites green at this commit;
`scripts/prove-core-api-live.sh` (driven by
`TestCoreAPILiveD44Proof` under `ESHU_D44_LIVE=1`) replays `/readyz`
200/503/200 per backend and byte-identical satellite relay legs against
live backends — see
`docs/internal/9669-removal/wave6-d44-live-proof-evidence.md`.

No-Observability-Change: the reorder changes response headers only and
the proof harness adds no operator surface; per-request relay rows
(via/status/outcome) already flow through the satellite log.
