# AGENTS.md — cmd/mock-github guidance for LLM assistants

## Read first

1. `go/cmd/mock-github/README.md` — purpose, configuration, invariants
2. `go/cmd/mock-github/server.go` — `Server`, the seven HTTP handlers, the
   one-time code store
3. `go/internal/githublogin/connector.go` — `githubConnector`, the production
   client this mock must stay compatible with (exact endpoint set and JSON
   shapes)
4. `go/internal/githublogin/provider_connection_test_probe.go` — the `GET /`
   reachability probe a compose-deployed provider config pointed at this
   mock must satisfy

## Invariants this package enforces

- **Single configured identity, no login UI** — `handleAuthorize` never
  prompts; it redirects to `redirect_uri` immediately with a fresh code.
  Mirrors `mock-oidc-idp`'s same invariant.
- **`/login/oauth/access_token` always 200s** — real GitHub returns HTTP 200
  for both a successful exchange and a failed one, signaling failure only
  through a non-empty `error` field. `handleAccessToken` must never switch to
  a 4xx status for a bad code; that would silently stop testing
  `githubConnector.Exchange`'s actual error-detection path (it checks
  `parsed.Error != ""`, not the status code).
- **Bearer-token gate on every REST identity call** — `requireBearer` checks
  `Authorization: Bearer mock-github-token` on `/user`, `/user/emails`,
  `/user/memberships/orgs`, `/user/teams`. Do not relax this to "any
  non-empty header": the F-9 leakage suite needs a real 401 for a
  credential-less or garbage-token call.
- **One-time codes** — `issueCode`/`consumeCode` in `server.go` delete a code
  on first redemption, mirroring `mock-oidc-idp`.
- **No OTEL bootstrap** — matches `mock-oidc-idp`'s precedent: do not add
  `internal/telemetry` wiring here.
- **Response shapes match `connector.go`'s unexported structs exactly** —
  `githubUser`, `githubEmail`, `githubOrgMembership`, `githubTeam` are JSON
  field names, not documentation; a field rename here without checking
  `connector.go` breaks `FetchIdentity` silently (it just gets a zero value,
  not an error, for most fields).

## Common changes and how to scope them

- **Add a second synthetic identity/persona** → today `ServerConfig.Identity`
  is one value read once at startup (`configFromEnv` in `main.go`), matching
  `mock-oidc-idp`'s pattern. Prefer a second env-configured container over
  request-time identity selection, for the same reason documented in
  `mock-oidc-idp/AGENTS.md`.
- **Add a second org or more team memberships** → `MOCK_GITHUB_TEAMS` already
  accepts a comma-separated list; `MOCK_GITHUB_ORG` is single-valued by
  design (see `ProviderConfig.AllowedOrgs`'s tenant-boundary role in
  `githublogin/service_types.go` — a real deployment allow-lists orgs, so one
  org per mock identity mirrors production shape for the F-5 shape-C E2E
  suite).
- **Support GitHub Enterprise Server's differing API base path** (`/api/v3`)
  → `EffectiveAPIBaseURL` in `githublogin/service.go` already resolves this
  on the CALLER side (a provider config sets an explicit `api_base_url`
  pointing at this mock); this binary itself does not need a GHES-specific
  mode, since docker-compose.e2e.yaml's provider config just points
  `base_url` AND `api_base_url` at the same mock instance.

## Failure modes and how to debug

- Symptom: `githubConnector.Exchange` returns `"github token exchange
  error: bad_verification_code"` → cause: the code was already redeemed
  once, or the process restarted and the in-memory `codes` map was cleared →
  codes do not survive a restart by design.
- Symptom: `FetchIdentity` returns empty `ActiveOrgs`/`TeamHandles` → cause:
  the caller's `allowedOrgs` argument does not (case-insensitively) match
  `MOCK_GITHUB_ORG`/the org half of `MOCK_GITHUB_TEAMS` — this mock always
  reports every configured org/team; `connector.go` filters client-side.
- Symptom: `probeAPIRoot` (Test-connection button) fails → cause: the
  provider config's `api_base_url` does not point at this container's
  reachable address (compose-network hostname from the `eshu`/`mcp-server`
  container's perspective, `--host-resolver-rules`-mapped hostname from the
  browser's).

## Anti-patterns specific to this package

- **Persisting codes or tokens to Postgres** — deliberately in-memory and
  stateless across restarts, like `mock-oidc-idp`.
- **Validating `client_secret`** — there is no client registry.
- **Adding a real login form** — defeats the point of a scripted,
  deterministic E2E fixture.

## What NOT to change without discussion

- The endpoint paths (`/login/oauth/authorize`, `/login/oauth/access_token`,
  `/user`, `/user/emails`, `/user/memberships/orgs`, `/user/teams`, `/`) — a
  compose-configured `ProviderConfig` pointed at this mock assumes exactly
  these paths relative to `base_url`/`api_base_url`.
- `mockAccessToken`'s fixed opaque value — `githublogin` never treats a
  GitHub access token as a JWT, so there is no reason to make it dynamic; a
  fixed value keeps a captured token replayable in a bug report, mirroring
  `mock-oidc-idp`'s static signing key rationale.
