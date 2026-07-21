# mock-oidc-idp

## Purpose

`mock-oidc-idp` is a minimal, self-contained OpenID Connect Authorization
Code identity provider. It exists only to give the local and CI browser-auth
E2E suites (issue #4971, epic #4962) a deterministic, credential-free OIDC
counterparty for the SSO login flow, so those suites can prove Eshu's
`oidclogin` package end-to-end without a live third-party IdP.

## Ownership boundary

This binary owns exactly four synthetic OIDC endpoints and one static signing
key. It does not own OIDC client behavior (that is
`go/internal/oidclogin/connector.go`, `NewOIDCConnector`), provider-config
storage or the group-to-role mapping (`go/internal/oidclogin/db_provider_config.go`,
`go/internal/query/admin_provider_config_*.go`), or browser session issuance
(`go/internal/query/oidc_login_handler.go`). It has no client registry, no
persistence, and no credential check: every `/authorize` call gets the one
identity this process was started with.

## Entry points

- `main` and `run` in `go/cmd/mock-oidc-idp/main.go`; single-process binary,
  no subcommands
- `eshu-mock-oidc-idp --version` prints the build-time version through
  `buildinfo.PrintVersionFlag` before opening a listener
- `Server.Mux` (`server.go`) mounts the four HTTP handlers:
  `GET /.well-known/openid-configuration`, `GET /authorize`, `POST /token`,
  `GET /jwks`

## Configuration

Environment variables read by `configFromEnv` (`main.go`):

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `MOCK_OIDC_ISSUER_URL` | yes | none | This IdP's own base URL as reachable by both the OIDC client and this process. Echoed into the discovery document's `issuer` field and the ID token's `iss` claim; both are validated by the client against the URL it used for discovery, so this must be the exact reachable URL, not a display name. |
| `MOCK_OIDC_LISTEN_ADDR` | no | `0.0.0.0:8080` | HTTP listen address. |
| `MOCK_OIDC_SUBJECT` | no | `member-user-1` | The `sub` claim of the minted ID token. |
| `MOCK_OIDC_EMAIL` | no | `member.user@example.test` | The `email` claim. |
| `MOCK_OIDC_GROUPS` | no | `member` | Comma-separated group claim values. |
| `MOCK_OIDC_GROUP_CLAIM` | no | `groups` | The claim name carrying the group list, matching the default `GroupsClaim` `oidclogin.ResolveSealedProviderConfig` assigns a DB-backed provider config. |
| `MOCK_OIDC_ACCESS_TOKEN_JWT` | no | `false` | When `true`/`1`, every `/token` response's `access_token` is a signed JWT (see below) even when the request carries no RFC 8707 `resource` parameter. |
| `MOCK_OIDC_ACCESS_TOKEN_AUDIENCE` | no | none | Fallback `aud` claim for a JWT access token when neither `/authorize` nor `/token` carried a `resource` value. |
| `MOCK_OIDC_ACCESS_TOKEN_TTL_SECONDS` | no | `600` | Lifetime of a minted JWT access token. A short value (e.g. `1`) drives a deterministic expired-token E2E probe. |

### JWT access tokens (issue #5170)

By default `/token` returns the fixed opaque string `"mock-access-token"` as
`access_token` — unchanged since #4971, so the browser-auth E2E suite (which
never reads `access_token`, only `id_token`) stays byte-stable.

When the `/authorize` or `/token` request carries an RFC 8707 `resource`
parameter, or `MOCK_OIDC_ACCESS_TOKEN_JWT=true` is set, `/token` instead mints
a signed RS256 JWT access token: `iss` = this IdP's issuer, `aud` = the
`resource` value (falling back to `MOCK_OIDC_ACCESS_TOKEN_AUDIENCE`), `sub`/
`email`/the configured group claim = the configured synthetic identity, `exp`
= now + `MOCK_OIDC_ACCESS_TOKEN_TTL_SECONDS`. This is what
`go/internal/oidcbearer`'s `Resolver` validates for MCP OAuth bearer calls
(`server.go`'s `mintAccessToken`/`signAccessToken`).

## Telemetry

The binary does not register OTEL providers or `eshu_dp_*` metrics; it logs
through the standard library `log/slog` package to stderr. This matches the
`admin-status` CLI's precedent (`go/cmd/admin-status`): a synthetic test
fixture with no database and no production telemetry contract does not need
the runtime OTEL bootstrap.

## Gotchas / invariants

- single identity, no login form: `/authorize` redirects to `redirect_uri`
  immediately with a fresh one-time code: there is nothing to choose between
- the RSA signing key is a fixed, committed, non-secret constant
  (`staticPrivateKeyPEM` in `keys.go`) so JWKS output and the derived `kid`
  stay stable across container restarts; it must never be reused outside
  this binary
- `/token` accepts client authentication via either HTTP Basic or
  `client_id`/`client_secret` POST fields but never checks the secret value:
  this is not a client registry
- `/token` validates `redirect_uri` against the value the matching
  `/authorize` call carried, matching real IdP behavior, but does not
  validate `client_id` against a registry
- issued codes are one-time use, held in an in-memory map guarded by a mutex;
  restarting the process discards all outstanding codes

## Performance & observability evidence

This binary is a **test-only** mock OIDC IdP: it is built solely for the #4971
auth E2E stack (`docker-compose.e2e.yaml`) and is deliberately kept out of the
released production image. It carries no product-runtime performance or
observability contract.

- No-Regression Evidence: it serves a single, fixed synthetic identity from an
  in-memory, mutex-guarded one-time-code map; there is no graph, queue, reducer,
  Cypher, or Postgres path, no batching or lease behavior, and no repo-scale
  data. Baseline and after are identical by construction — the change adds a new
  test fixture binary, it does not alter any product hot path. The only consumer
  is the browser E2E gate, whose end-to-end wall clock is ~11.5s for 17 steps on
  a fresh stack (`e2e-artifacts/auth-e2e-report.json`).
- No-Observability-Change: it emits only its own request logs to stdout for E2E
  debugging; it adds no product metrics, spans, or status surfaces, and an
  operator never runs it. No `eshu_*` instrument or dashboard is affected.

## Related docs

- [Docker Compose deployment](../../../docs/public/run-locally/docker-compose.md)
- `go/internal/oidclogin/README.md` — the production OIDC login service this
  binary's output is verified against
- `go/internal/oidclogin/db_provider_config.go` — the DB-backed
  `ProviderConfig` shape (`issuer`, `client_id`, `group_claim`,
  `redirect_url`) this mock IdP's discovery/token/jwks output must satisfy
