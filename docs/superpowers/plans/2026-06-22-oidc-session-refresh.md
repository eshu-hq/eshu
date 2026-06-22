# OIDC Session Refresh Window Plan

Issue: #3529

## Goal

Bound OIDC-backed dashboard-session staleness without persisting raw provider
tokens, raw group names, emails, or provider claim bodies. The first shipped
slice forces reauthentication through the IdP within a configurable window by
capping OIDC-issued browser-session absolute expiry.

## Flow

1. OIDC login starts through `query.OIDCLoginHandler.handleStart`.
2. `oidclogin.Service` validates state, nonce, issuer/JWKS, subject, redirect,
   and group-to-role grants.
3. `query.OIDCLoginHandler.handleCallback` issues browser-session cookies
   through `BrowserSessionHandler`.
4. Postgres stores only hashes and opaque auth bounds in `browser_sessions`.
5. Later requests resolve sessions through `ResolveBrowserSession`, which
   already denies expired, revoked, inactive, or policy-drifted sessions.

## Implementation Steps

1. Add failing tests proving:
   - OIDC callback caps browser-session absolute expiry to a refresh window.
   - Browser-session creation clamps idle expiry to absolute expiry when the
     configured absolute window is shorter.
   - API wiring parses a new OIDC refresh-window env var and fails closed on
     invalid values.
2. Add the refresh-window env constant and API wiring.
3. Update `BrowserSessionHandler` to issue sessions with idle expiry capped by
   absolute expiry.
4. Update docs and env registry, then regenerate generated env docs.
5. Run focused tests, docs/package gates, private-data scan, self-review, then
   push the stacked branch only after PR #3543 remains current.

## Evidence Gates

- `go test ./internal/query -run 'Test(OIDCLoginHandlerCallback|BrowserSessionHandlerCreate)' -count=1`
- `go test ./cmd/api -run TestNewOIDCLoginHandler -count=1`
- `ESHU_UPDATE_ENV_DOC=1 go test ./internal/envregistry -run TestEnvRegistryReferenceDocUpToDate -count=1`
- `go test ./cmd/api ./internal/query ./internal/envregistry -count=1`
- `scripts/verify-package-docs.sh`
- `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`
- `git diff --check`
