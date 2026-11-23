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
- Commit listener (dark unless `ESHU_COMMIT_LISTEN` is set to an empty-host
  or literal-IP `host:port`, e.g. `0.0.0.0:8089`; hostnames are refused):
  `ESHU_COMMIT_MANIFEST_PATH`, `ESHU_COMMIT_CLAIM_PLANE_URL` (bare
  http/https base URL), `ESHU_COMMIT_SOURCE_SYSTEM`,
  `ESHU_COMMIT_SCOPE_KIND` (presence-checked only; an unlisted spelling
  passes startup and mismatches are refused per-request by admission),
  `ESHU_COMMIT_INSTANCE_ID` (all required when
  the listener is set), and optional `ESHU_COMMIT_GRANTS_PATH`. Serves
  `POST /workflow.v1/CommitClaimFacts` through the claim-plane fence, so
  the satellite proxy finally has a live receiving end (issue #11064).

## Probes

- `GET /health` — shallow process check (`{"status":"ok"}`), public.
- `GET /readyz` — backend-gated: 200 only when both Postgres and Neo4j
  answer, else the permanent 503 with no `Retry-After`, public.

## Telemetry

No-Observability-Change: the binary reuses the shared `eshu_dp_*`
instruments (`telemetry.NewInstruments`) and the process logger; the only
addition is the one startup event `runtime.commit_listener.listening`
(with `addr`) when the commit listener is enabled. The Prometheus handler
serves on `/metrics` (public, behind the same auth chain).

## Performance Evidence: commit listener (issue #11064)

- Baseline: no :8089 listener existed, so every CommitClaimFacts call
  failed at dial time (connection refused); there is no prior
  per-request cost to regress against.
- After: one bounded second HTTP server serving only the
  `workflow.v1/CommitClaimFacts` protocol route through the pre-existing
  `extensionhost` commit handler and claim-plane fence. No new hot
  loops, no Cypher/SQL schema or index changes, no new worker pools;
  when `ESHU_COMMIT_LISTEN` is unset the primary serve path is
  byte-identical to before (dark by default: `TestCommitListenerDarkByDefault`
  pins the config reports nil, and the `TestJoinCoreServersDarkLane*`
  lifecycle tests prove the join drains with a nil commit channel).
- Input shape: framed client envelopes capped at
  `workflow.MaxCommitClaimFactsBytes`; handler ack happens only after
  the commit is durable. Join shutdown (`joinCoreServers`) drains both
  servers on cancel and reports the first fatal error — proven by the
  `TestJoinCoreServers*` lifecycle tests.
- Telemetry: one startup event (`runtime.commit_listener.listening`
  with `addr`); per-request rows flow through the existing satellite
  log. Observability Evidence: the startup event above, present only
  when the listener is enabled.

## Rollback plan: commit listener (issue #11064)

- To take the listener out of service, revert the IaC core pin to the
  last-known-good running pin through the normal pin PR and ArgoCD sync;
  unsetting `ESHU_COMMIT_LISTEN` keeps the lane dark by default.
- No state to migrate: the listener is stateless intake, and the claim
  plane retries through `FailClaimRetryable` while the endpoint is down,
  so claims queue instead of dropping.

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
