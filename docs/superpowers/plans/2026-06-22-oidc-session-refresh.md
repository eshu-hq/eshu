# OIDC Session Refresh Window Plan

Issue: #3529

## Goal

Bound OIDC-backed dashboard-session staleness without persisting raw provider
tokens, raw group names, emails, or provider claim bodies. The first shipped
slice forces reauthentication through the IdP within a configurable window by
persisting a hash-only `external_auth_stale_after` timestamp and revoking stale
OIDC-backed browser sessions before returning an auth context.

## Flow

1. OIDC login starts through `query.OIDCLoginHandler.handleStart`.
2. `oidclogin.Service` validates state, nonce, issuer/JWKS, subject, redirect,
   and group-to-role grants.
3. `query.OIDCLoginHandler.handleCallback` issues browser-session cookies
   through `BrowserSessionHandler`.
4. Postgres stores only hashes, opaque auth bounds, and external proof
   timestamps in `browser_sessions`.
5. Later requests resolve sessions through `ResolveBrowserSession`, which
   revokes stale OIDC-backed sessions before the normal expired, revoked,
   inactive, or policy-drifted session checks can return auth.

## Implementation Steps

1. Add failing tests proving:
   - OIDC callback stamps hash-only external proof metadata and stale-after
     bounds.
   - Browser-session creation clamps idle expiry to absolute expiry when the
     configured absolute window is shorter.
   - API wiring parses a new OIDC refresh-window env var and fails closed on
     invalid values.
2. Add the refresh-window env constant and API wiring.
3. Update `BrowserSessionHandler` and the Postgres adapter to carry only
   provider config id, subject hash, proof time, and stale-after timestamps.
4. Revoke stale OIDC-backed sessions atomically before session resolution
   returns an auth context.
5. Update docs and env registry, then regenerate generated env docs.
6. Run focused tests, docs/package gates, private-data scan, and self-review
   before push.

## Evidence Gates

- `go test ./internal/query -run 'Test(OIDCLoginHandlerCallback|BrowserSessionHandlerCreate)' -count=1`
- `go test ./cmd/api -run TestNewOIDCLoginHandler -count=1`
- `go test ./internal/storage/postgres -run 'Test(BootstrapDefinitionsIncludeBrowserSessions|BrowserSessionStoreCreatesOIDCRefreshProofMetadata|BrowserSessionStoreRevokesStaleOIDCSessionBeforeReturningAuth|BrowserSessionStoreRevokesStaleOIDCSessionBeforeCSRFFailure)' -count=1`
- `ESHU_UPDATE_ENV_DOC=1 go test ./internal/envregistry -run TestEnvRegistryReferenceDocUpToDate -count=1`
- `go test ./cmd/api ./internal/query ./internal/oidclogin ./internal/storage/postgres ./internal/envregistry -count=1`
- `go vet ./cmd/api ./internal/query ./internal/oidclogin ./internal/storage/postgres ./internal/envregistry`
- `scripts/verify-package-docs.sh`
- `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`
- `git diff --check`

## Runtime Evidence

No-Regression Evidence: #3529 adds nullable, hash-only external provider proof
columns to `browser_sessions` and performs one guarded stale-session revocation
before normal browser-session resolution. The stale path is idempotent under
duplicate delivery because already-revoked rows are excluded by
`revoked_at IS NULL`; stale sessions return no auth context, while non-stale
sessions continue through the existing tenant, workspace, expiry, CSRF, and
policy-revision predicates. Verification covers OIDC proof stamping, invalid
refresh-window startup failures, idle/absolute expiry clamping, hash-only
provider proof persistence, stale-session revocation before auth return, stale
revocation before unsafe-request CSRF classification, schema bootstrap parity,
and the broader API/query/OIDC/storage/envregistry packages.

No-Observability-Change: the change adds no route, worker, queue, graph write,
metric instrument, metric label, span name, runtime process, or provider API
call. Operators diagnose the path through existing API request metrics and HTTP
route attribution, governance-audit denied read events with reason
`oidc_session_reauth_required`, existing `InstrumentedDB` Postgres exec/query
spans and `eshu_dp_postgres_query_duration_seconds`, and the durable
`browser_sessions.revoked_at` row state. The stale-session branch stores only
provider config id, provider subject hash, validation time, and stale-after
time; it does not persist raw provider tokens, raw groups, claim bodies,
private endpoints, or private deployment identifiers.
