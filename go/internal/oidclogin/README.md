# OIDC Login

## Purpose

`oidclogin` handles backend OIDC Authorization Code login for dashboard browser
sessions. It validates provider-issued ID tokens, maps external group claims
through Eshu-owned roles, and returns a bounded `query.AuthContext` for the
existing browser-session issuer.

## Ownership boundary

This package owns OIDC state, nonce, provider exchange, ID token verification,
and group-to-role grant resolution for login. It does not enforce query route
authorization, write browser-session rows directly, manage IdP admin APIs, or
store provider tokens. Browser-session replay fails closed when Eshu workspace
policy revisions change; IdP-driven active-session refresh is tracked
separately from this login package.

## Exported surface

See `doc.go` for the package contract. The main exported types are `Service`,
`Config`, `ProviderConfig`, `StateStore`, `GrantResolver`, `Connector`,
`StaticGrantResolver`, and `LoadConfigFile`.

## Dependencies

The package depends on `internal/query` for auth context and handler error
contracts. The real connector uses `github.com/coreos/go-oidc/v3/oidc` for
issuer discovery, JWKS-backed ID token verification, and audience/expiry checks,
plus `golang.org/x/oauth2` for Authorization Code exchange. Storage and
browser-session writes stay behind interfaces supplied by `cmd/api`.

## Telemetry

This package emits no spans or metrics directly. API route metrics and
Postgres-store instrumentation live in `cmd/api` and
`internal/storage/postgres`.

## Gotchas / invariants

Only hashes of state, nonce, redirect URI, subject, and group claim values may
enter durable storage. Raw ID tokens, access tokens, group names, email
addresses, and client secrets stay transient. Login fails closed when nonce
validation, provider verification, group mapping, role grants, tenant/workspace
state, or policy revision resolution fails.

Group claims are never permissions. They map to Eshu role IDs first; roles then
resolve to explicit scope and repository grants.

Login-time grant resolution is not an IdP polling or refresh loop. Existing
OIDC-backed browser sessions depend on browser-session revocation, expiry, and
workspace policy-revision drift until a dedicated active-session refresh worker
ships.

## Related docs

- `docs/internal/design/3452-user-management-identity-federation.md`
- `docs/public/reference/http-api.md`
- `docs/public/reference/authorization-catalog.md`
