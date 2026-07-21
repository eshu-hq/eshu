# mock-github

## Purpose

`mock-github` is a minimal, self-contained stand-in for github.com's OAuth2
web-application flow and REST identity endpoints. It exists only to give the
F-9 (issue #5170) auth-mcp E2E stack a deterministic, credential-free GitHub
counterparty, so that suite can prove Eshu's `githublogin` package end-to-end
without depending on a live GitHub App or GitHub Enterprise Server instance.

## Ownership boundary

This binary owns exactly the endpoint set
`go/internal/githublogin/connector.go`'s `githubConnector` calls, plus the
unauthenticated reachability probe the admin console's Test-connection
button uses. It does not own GitHub OAuth client behavior (that is
`connector.go`), provider-config storage or the org/team-to-role mapping
(`go/internal/githublogin/db_provider_config.go`,
`go/internal/query/admin_provider_config_*.go`), or browser session issuance.
It has no client registry, no persistence, and no credential check: every
login gets the one identity this process was started with.

## Entry points

- `main` and `run` in `go/cmd/mock-github/main.go`; single-process binary, no
  subcommands
- `eshu-mock-github --version` prints the build-time version through
  `buildinfo.PrintVersionFlag` before opening a listener
- `Server.Mux` (`server.go`) mounts the seven HTTP handlers: `GET
  /login/oauth/authorize`, `POST /login/oauth/access_token`, `GET /user`,
  `GET /user/emails`, `GET /user/memberships/orgs`, `GET /user/teams`, `GET /`

## Configuration

Environment variables read by `configFromEnv` (`main.go`). Unlike
`mock-oidc-idp`'s `MOCK_OIDC_ISSUER_URL`, none is required — this mock never
echoes a caller-supplied issuer/audience back to itself.

| Variable | Default | Purpose |
| --- | --- | --- |
| `MOCK_GITHUB_LISTEN_ADDR` | `0.0.0.0:8080` | HTTP listen address. |
| `MOCK_GITHUB_LOGIN` | `e2e-github-user` | The `login` field of `GET /user`. |
| `MOCK_GITHUB_USER_ID` | `1001` | The numeric `id` field of `GET /user`. |
| `MOCK_GITHUB_EMAIL` | `e2e-github-user@example.test` | The verified primary email `GET /user/emails` returns. |
| `MOCK_GITHUB_ORG` | `eshu-e2e-org` | The org `GET /user/memberships/orgs` reports an ACTIVE membership in. |
| `MOCK_GITHUB_TEAMS` | `eshu-e2e-org/platform-team` | Comma-separated `org/slug` pairs `GET /user/teams` returns. |

## Telemetry

The binary does not register OTEL providers or `eshu_dp_*` metrics; it logs
through the standard library `log/slog` package to stderr, matching
`mock-oidc-idp`'s precedent: a synthetic test fixture with no database and no
production telemetry contract does not need the runtime OTEL bootstrap.

## Gotchas / invariants

- single identity, no login form: `/login/oauth/authorize` redirects to
  `redirect_uri` immediately with a fresh one-time code
- `/login/oauth/access_token` ALWAYS returns HTTP 200, even for an unknown or
  reused code — it responds with a non-empty `error` field instead, matching
  real GitHub's documented behavior (`githubTokenResponse` in
  `connector.go`). `githubConnector.Exchange` treats the `error` field, not
  the status code, as the failure signal.
- `GET /user`, `/user/emails`, `/user/memberships/orgs`, and `/user/teams`
  all require `Authorization: Bearer mock-github-token` (the one fixed opaque
  token every successful exchange returns) and 401 otherwise
- `GET /user/memberships/orgs` and `GET /user/teams` return their fixture
  data only on page 1 (or when `page` is unset); any later page is empty, so
  `connector.go`'s pagination loop naturally terminates after one page
- `GET /` requires no authentication, matching real `api.github.com`'s root
  and `provider_connection_test_probe.go`'s `probeAPIRoot`
- issued codes are one-time use, held in an in-memory map guarded by a mutex;
  restarting the process discards all outstanding codes

## Performance & observability evidence

This binary is a **test-only** mock GitHub counterparty: it is built solely
for the F-9 auth-mcp E2E stack (`docker-compose.e2e.yaml`) and is
deliberately kept out of the released production image, mirroring
`mock-oidc-idp`.

- No-Regression Evidence: it serves a single, fixed synthetic identity from
  an in-memory, mutex-guarded one-time-code map; there is no graph, queue,
  reducer, Cypher, or Postgres path. The only consumer is the F-9 E2E gate.
- No-Observability-Change: it emits only its own request logs to stderr for
  E2E debugging; it adds no product metrics, spans, or status surfaces, and
  an operator never runs it. No `eshu_*` instrument or dashboard is affected.

## Related docs

- [Docker Compose deployment](../../../docs/public/run-locally/docker-compose.md)
- `go/internal/githublogin/README.md` — the production GitHub login service
  this binary's output is verified against
- `go/internal/githublogin/connector.go` — the exact endpoint set and response
  shapes (`githubUser`, `githubEmail`, `githubOrgMembership`, `githubTeam`)
  this mock's handlers satisfy
- `go/internal/githublogin/provider_connection_test_probe.go` — the
  reachability probe `GET /` satisfies
