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
store provider tokens. It returns only the Eshu auth context plus hash-only
provider proof metadata; browser-session replay, expiry, policy revision drift,
and provider-proof stale-window enforcement live in `internal/query`,
`cmd/api`, and `internal/storage/postgres`.

This package also owns the bounded active-session revocation refresh engine
(`Refresher`). It decides per stale session whether to extend the proof window
or revoke, but it depends on `SessionRefreshStore`, `RoleGrantResolver`, and
`ExternalSubjectLookup` ports implemented in `internal/storage/postgres` and
wired (with cadence, batch bound, and telemetry) in `cmd/api`.

## Exported surface

See `doc.go` for the package contract. The main exported types are `Service`,
`Config`, `ProviderConfig`, `StateStore`, `GrantResolver`, `Connector`,
`StaticGrantResolver`, and `LoadConfigFile`, plus the active-session refresh
surface `Refresher`, `RefreshConfig`, `RefreshOutcome`, `SessionRefreshStore`,
`RoleGrantResolver`, `ExternalSubjectLookup`, `StaleSession`, and
`SessionAuthProofUpdate`.

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

`CompleteOIDCLogin` wraps every denied/unavailable return with
`query.SSOLoginDeniedError`, carrying a stable reason classification
(`state_invalid`, `redirect_mismatch`, `code_exchange_failed`,
`id_token_invalid`, `nonce_mismatch`, `subject_missing`, `no_group_claim`,
`no_grants`, `state_store_unavailable`, `grant_resolution_unavailable`).
`query.OIDCLoginHandler` uses that classification to record an
`identity_authentication` governance-audit event for every callback outcome
— success (`sso_login_authenticated`) and denial alike — closing the gap
issue #5601 found: an OIDC sign-in previously left no durable, queryable
trace once its `browser_sessions` row expired.

## Gotchas / invariants

Only hashes of state, nonce, redirect URI, subject, and group claim values may
enter durable storage. Raw ID tokens, access tokens, group names, email
addresses, and client secrets stay transient. Login fails closed when nonce
validation, provider verification, group mapping, role grants, tenant/workspace
state, or policy revision resolution fails.

Group claims are never permissions. They map to Eshu role IDs first; roles then
resolve to explicit scope and repository grants.

`StaticGrantResolver` never populates `GrantResolution.PolicyRevisionHash`
(#5038), even when a static config file's `role_grants[].policy_revision_hash`
is set. That field is deprecated and ignored: an operator-supplied value could
silently drift from the live workspace policy revision hash, and every
subsequent authenticated request for that session would 401 even though login
itself succeeded. Leaving the field empty lets the browser-session store
default it to the live workspace hash at session-create time, matching the
DB-backed group-mapping resolver's already-safe behavior.

Login-time grant resolution is not an IdP polling loop. OIDC-backed browser
sessions store hash-only provider proof metadata and force provider
reauthentication after the configured stale window; the login package only
supplies the provider config id, subject hash, and proof time needed by the
browser-session layer.

## Related docs

- `docs/internal/design/3452-user-management-identity-federation.md`
- `docs/public/reference/http-api.md`
- `docs/public/reference/authorization-catalog.md`
