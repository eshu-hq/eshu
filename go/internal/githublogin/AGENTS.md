# AGENTS — internal/githublogin

GitHub Authorization Code (plain OAuth2, non-discovery) login for dashboard
browser sessions. Read `README.md` and `doc.go` before editing.

## Invariants

- Store only hashes for state, provider key, base URL, client ID, redirect
  URI, subject, and team handles. Never persist raw access tokens, GitHub
  logins, verified emails, client secrets, or REST API response bodies.
- Login must fail closed. Do not fall back to shared-token, all-scope, or
  partially mapped access when code exchange, identity fetch, verified-email
  presence, allowed-org membership, or team→role grant resolution fails.
- Every denial reason (`code_exchange_failed`, `identity_fetch_failed`,
  `email_not_verified`, `org_not_allowed`, `no_team_role_mapping`,
  `no_role_grant`) must stay a distinct, auditable `slog` line — do not
  collapse them into one generic message.
- `AllowedOrgs` must remain mandatory and non-empty on every
  `ProviderConfig`. Do not add a code path that lets a provider with an
  empty allow-list authenticate any GitHub account.
- Team handles ("org/team-slug") must only be hashed and fed into grant
  resolution for teams whose org is already in `AllowedOrgs`. Do not widen
  this to hash every team the token can see.
- GitHub team/group hashes must go through the SAME
  `oidclogin.GrantResolver`/`GrantQuery`/`GrantResolution` seam an OIDC group
  claim uses (see service_types.go's type aliases) — do not fork a parallel
  grant-resolution concept for GitHub.
- Callback state is one-time-use. Duplicate or replayed callbacks must not
  create another browser session.
- Errors returned to HTTP callers must be generic and must not include
  GitHub API responses, token material, org/team names, or customer
  identifiers.

## Boundaries

- This package returns `query.AuthContext` (via `CompleteResponse.Auth`); it
  does not write cookies or browser-session rows. Browser-session issuance
  lives in `internal/query` and `cmd/api`.
- Durable state storage lives in `internal/storage/postgres`.
- GitHub OAuth2/REST calls use only the standard library `net/http`. Do not
  introduce a third-party OAuth2 client library for this package — GitHub's
  endpoints are simple enough that hand-rolling them (verified against
  GitHub's own REST/OAuth documentation) is both correct and dependency-free.
- Do not import `internal/oidclogin`'s OIDC-specific types (`ProviderConfig`,
  `Connector`, `VerifiedClaims`, discovery/JWKS logic) — only the
  grant-resolution seam aliased in service_types.go.

## Tests

`service_test.go` covers state hashing (no nonce — plain OAuth2 has none),
redirect-URI drift denial, verified-email enforcement, allowed-org
enforcement (including case-insensitivity and org-scoped team filtering),
and team→role grant mapping through the shared `StaticGrantResolver`, proven
equivalent to the OIDC group→role fixture in
`internal/oidclogin/service_test.go`. `connector_test.go` covers the OAuth2
code exchange and REST identity/org/team fetch against a fake HTTP server
(GitHub token-exchange success/error shapes, `/user`, `/user/emails`,
`/user/memberships/orgs`, `/user/teams` pagination). `config_test.go` covers
`LoadConfigFile`'s validation (missing allow-list, malformed grant config).
Add a focused test for any new claim, provider field, grant, or denial
reason before changing production code.
