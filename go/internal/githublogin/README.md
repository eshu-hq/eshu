# GitHub Login

## Purpose

`githublogin` handles backend GitHub Authorization Code (plain OAuth2) login
for dashboard browser sessions (issue #5166, F-5 of epic #5161, "GitHub
sign-in connector"). It exchanges an authorization code for a GitHub access
token, resolves verified identity, active org membership, and team
membership entirely through the GitHub REST API, enforces an operator
allow-list of GitHub orgs, maps team membership to Eshu roles, and returns a
bounded `query.AuthContext` for the existing browser-session issuer — the
same shape `internal/oidclogin` produces for an OIDC login.

## Why not `internal/oidclogin`

github.com (and GitHub Enterprise Server) publish no
`/.well-known/openid-configuration` discovery document, issue no ID token,
and expose no JWKS. `oidclogin`'s connector (`coreos/go-oidc`) is
discovery-based and cannot authenticate against GitHub at all. This package
is a dedicated, non-discovery OAuth2 connector: it performs the
authorization-code exchange by hand (`https://github.com/login/oauth/...`,
or a GitHub Enterprise Server host) and calls the GitHub REST API
(`/user`, `/user/emails`, `/user/memberships/orgs`, `/user/teams`) with the
resulting access token to build identity.

## Ownership boundary

This package owns GitHub OAuth2 state, code exchange, REST identity/org/team
lookup, and the org allow-list + team→role grant resolution for login. It
does not enforce query route authorization, write browser-session rows
directly, manage IdP admin APIs, or store provider tokens. It returns only
the Eshu auth context plus hash-only provider proof metadata, exactly like
`oidclogin`. Browser-session replay, expiry, and policy revision drift
enforcement live in `internal/query`, `cmd/api`, and
`internal/storage/postgres`.

## Exported surface

See `doc.go` for the package contract. The main exported types are
`Service`, `Config`, `ProviderConfig`, `StateStore`, `Connector`,
`ConnectorFactory`, `NewGitHubConnector`, `LoadConfigFile`, and the reused
`oidclogin` grant-resolution seam: `GrantResolver`, `GrantQuery`,
`GrantResolution`, `StaticGrantResolver`, `GroupRoleMapping`, `RoleGrant`
(all type aliases — see service_types.go).

## Dependencies

The package depends on `internal/query` for `AuthContext` and on
`internal/oidclogin` for the shared grant-resolution types only (no OIDC
discovery or ID-token logic is imported). The real connector uses only the
standard library `net/http` — GitHub's OAuth2 endpoints and REST API need no
third-party OAuth2/OIDC client library.

## Telemetry

This package emits no spans or metrics directly, matching `oidclogin`'s
convention: API route metrics live in `cmd/api`. It does emit structured
`slog` WARN lines with a stable `reason` field
(`code_exchange_failed`, `identity_fetch_failed`, `email_not_verified`,
`org_not_allowed`, `no_team_role_mapping`, `no_grants`) for every denied
login, which is the audited-denial trail issue #5166 requires ("user
outside allowed orgs is rejected with an audited denied reason").

`CompleteGitHubLogin` also wraps every denied/unavailable return with
`query.SSOLoginDeniedError`, carrying the same reason classification (plus
`state_invalid`, `redirect_mismatch`, `subject_missing`,
`state_store_unavailable`, and `grant_resolution_unavailable`, which have no
slog line since they precede any identity fetch). `query.GitHubLoginHandler`
uses that classification to record an `identity_authentication`
governance-audit event for every callback outcome — success and denial alike
— which is the durable, queryable trail issue #5601 requires: the slog line
above rotates out of logs, the audit row does not.

## Gotchas / invariants

Only hashes of state, provider key, base URL, client ID, redirect URI, and
team handles ever enter durable storage or get logged. Raw access tokens,
GitHub logins, verified emails, and team/org names stay transient
in-process for the duration of one login. Login fails closed at every step:
state consumption, code exchange, verified-email presence, allowed-org
membership, and team→role grant resolution.

`AllowedOrgs` is mandatory and non-empty on every `ProviderConfig`
(`ValidateConfig` rejects an empty allow-list). Unlike an OIDC provider,
which trusts the IdP's own tenant boundary, a GitHub OAuth App can
authenticate any GitHub account on `BaseURL` — the org allow-list is the
only tenant boundary this connector has.

Team membership is only resolved, and only fed into grant resolution, for
teams whose org is in `AllowedOrgs` — a team in a non-allowed org is dropped
before hashing, so a stray mapping row can never grant access through an org
outside the connector's boundary.

## MCP posture (recorded decision)

A GitHub access token is opaque: unlike an OIDC bearer token there is no
issuer, audience, or JWKS to validate it against, and github.com is not an
MCP-spec (RFC 9728) authorization server. GitHub-backed orgs therefore use
per-user issued API tokens for MCP access (issue #5164, F-3), not GitHub
OAuth tokens presented as MCP bearer credentials — the sign-in posture and
docs state this explicitly rather than implying GitHub sign-in also unlocks
MCP OAuth. Accepting GitHub Actions OIDC tokens (issuer
`token.actions.githubusercontent.com`, a real OIDC issuer, unlike
github.com's user login) as CI service-principal credentials through F-1's
bearer-token resolver is a distinct, deferred decision — recorded here, not
implemented by this package.

## Related docs

- `docs/internal/design/3452-user-management-identity-federation.md`
- `docs/public/reference/http-api.md`
- `docs/public/reference/authorization-catalog.md`
