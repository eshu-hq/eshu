// api/adminProviderConfigEndpoints.ts
// Client-side id generation and IdP-facing endpoint URI helpers for the
// DB-backed provider-config admin surface (#4966/#4967, GitHub #5166). Split
// out of adminProviderConfig.ts to keep that file under the 500-line cap;
// adminProviderConfig.ts re-exports every function here so existing import
// sites (ProviderConfigDrawer, providerConfigForm, AdminProvidersPanel, and
// the tests) are unaffected. These helpers are pure and carry no dependency
// on the read/write types in adminProviderConfig.ts.

// newClientProviderConfigId generates an opaque id in the Add drawer, BEFORE
// the first Save, so the SAML ACS URL / SP entity id (both path-scoped by
// provider_config_id — go/internal/samlauth/db_provider_config_test.go) can be
// rendered read-only for the operator to copy into their IdP immediately, not
// only after a round trip. The backend's create route accepts a caller-
// supplied provider_config_id verbatim (adminProviderConfigWriteRequest's
// ProviderConfigID doc comment), so this id is sent as-is on create.
//
// Entropy comes from crypto.getRandomValues() only — never Math.random(),
// which is not cryptographically secure and is flagged by CodeQL in any
// security-relevant path (this id becomes part of a URL registered with an
// external IdP, so collisions/predictability matter). getRandomValues is
// used directly (not crypto.randomUUID(), which wraps the same primitive)
// so there is a single, unconditional entropy source with no fallback
// branch that could silently degrade to a weaker one.
export function newClientProviderConfigId(): string {
  const bytes = new Uint8Array(16);
  globalThis.crypto.getRandomValues(bytes);
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
  return `pc_${hex}`;
}

function normalizeBase(baseUrl: string): string {
  return baseUrl.endsWith("/") ? baseUrl.slice(0, -1) : baseUrl;
}

// oidcRedirectUri is the single, deployment-wide OIDC callback every DB-backed
// OIDC provider config must register with its IdP — confirmed as a fixed
// route (not per-provider) in go/internal/oidclogin/service.go /
// db_provider_config_test.go, which set the same callback URL across
// providers. This is the exact value submitted as `redirect_url`.
export function oidcRedirectUri(baseUrl: string): string {
  return `${normalizeBase(baseUrl)}/api/v0/auth/oidc/callback`;
}

// githubCallbackUri is the single, deployment-wide GitHub OAuth2 callback
// every DB-backed GitHub provider config must register as the Authorization
// callback URL in its GitHub OAuth App (issue #5166, F-5) — a fixed route
// (GET /api/v0/auth/github/callback), not per-provider, mirroring
// oidcRedirectUri. This is the exact value submitted as `redirect_url`; the
// backend's githublogin connector requires a non-empty redirect_url to build
// the authorize URL.
export function githubCallbackUri(baseUrl: string): string {
  return `${normalizeBase(baseUrl)}/api/v0/auth/github/callback`;
}

// samlAcsUrl is Eshu's per-provider SAML Assertion Consumer Service URL —
// confirmed path shape in go/internal/samlauth/db_provider_config_test.go
// ("/api/v0/auth/saml/providers/{id}/acs"). This is the exact value submitted
// as `service_provider_acs_url`.
export function samlAcsUrl(baseUrl: string, providerConfigId: string): string {
  return `${normalizeBase(baseUrl)}/api/v0/auth/saml/providers/${encodeURIComponent(providerConfigId)}/acs`;
}

// samlServiceProviderEntityId derives a stable, deterministic SP entity id
// from the same per-provider path as samlAcsUrl (minus the /acs suffix). The
// backend does not compute or validate this value server-side — it is a
// free-text admin-supplied field (go/internal/samlauth/db_provider_config.go
// only requires it be non-empty) — so this is a UI convention chosen to keep
// the field read-only and copy-pasteable per #4967's acceptance criteria,
// not a backend-enforced identifier. An operator who needs a different SP
// entity id can still register a DB row directly against the API.
export function samlServiceProviderEntityId(baseUrl: string, providerConfigId: string): string {
  return `${normalizeBase(baseUrl)}/api/v0/auth/saml/providers/${encodeURIComponent(providerConfigId)}`;
}
