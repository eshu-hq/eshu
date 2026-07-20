// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// DefaultOAuthChallengeScope is the space-delimited OAuth scope string
// advertised both on a gated 401's WWW-Authenticate challenge (see
// PostureOAuthChallengePolicy) and as the discovery document's
// scopes_supported (issue #5163, F-2). It matches oidclogin's own default
// requested scope set (db_provider_config.go, service.go) rather than
// inventing a second scope decision for the same identity provider:
// internal/oidcbearer's resolver requires a verified token's "groups" claim
// to resolve any grant at all (ResolveScopedToken denies with no matching
// grant otherwise), so a client MUST request at least "groups" — bundled
// here with the standard OIDC identity scopes — to ever obtain an access
// token this resolver can honor.
const DefaultOAuthChallengeScope = "openid profile email groups"

// OAuthProtectedResourceMetadata is the RFC 9728 OAuth 2.0 Protected
// Resource Metadata document Eshu's MCP/API surface serves at
// /.well-known/oauth-protected-resource once an identity provider is
// configured (issue #5163, F-2, epic #5161). Field names and JSON shape
// follow RFC 9728 section 2 exactly (https://www.rfc-editor.org/rfc/rfc9728.html):
// "resource" is the only REQUIRED field; every other field here is RFC
// 9728 OPTIONAL or RECOMMENDED and is included only when the deployment has
// a real value for it — this document never advertises a phantom
// capability Eshu does not actually support (no jwks_uri: Eshu is never its
// own authorization server; no DPoP or mTLS fields: neither is implemented).
type OAuthProtectedResourceMetadata struct {
	// Resource is the protected resource's canonical identifier (RFC 8707),
	// the SAME value F-1's oidcbearer.Config.Audience validates a token's
	// "aud" claim against (ESHU_AUTH_RESOURCE_URI) — one canonical resource
	// URI shared between token validation and this document, so a client
	// that discovers this document and requests a token for Resource always
	// gets one the resolver accepts.
	Resource string `json:"resource"`
	// AuthorizationServers names the issuer(s) a client should use to obtain
	// a token for Resource, sourced from OAuthAuthorizationServerLister
	// (internal/oidcbearer.Resolver.ActiveIssuers in production) — never
	// from AuthProviderItem, which deliberately omits IssuerURL for the
	// login-picker's pre-auth privacy contract (see auth_providers_handler.go).
	// Omitted (empty) when no issuer lister is wired or none is currently
	// active, per RFC 9728's OPTIONAL status for this field.
	AuthorizationServers []string `json:"authorization_servers,omitempty"`
	// BearerMethodsSupported names how a client may present its access
	// token. Eshu's AuthMiddleware only ever reads Authorization: Bearer —
	// never a body or query-string token — so this is always exactly
	// ["header"].
	BearerMethodsSupported []string `json:"bearer_methods_supported,omitempty"`
	// ScopesSupported is the RFC 9728 RECOMMENDED hint of OAuth scope values
	// this resource accepts; see DefaultOAuthChallengeScope's doc comment
	// for why "groups" is load-bearing here, not decorative.
	ScopesSupported []string `json:"scopes_supported,omitempty"`
	// ResourceName is the RFC 9728 RECOMMENDED human-readable name shown by
	// a client's consent/connection UI.
	ResourceName string `json:"resource_name,omitempty"`
	// ResourceDocumentation is the RFC 9728 OPTIONAL URL of human-readable
	// documentation for this resource. Config-fed (ESHU_AUTH_RESOURCE_DOCUMENTATION);
	// omitted when unset.
	ResourceDocumentation string `json:"resource_documentation,omitempty"`
	// EshuPreregisteredClientID is an RFC 9728 section 2 extension member
	// (extension members are explicitly permitted): the OAuth client_id a
	// deployment has pre-registered with its authorization server for MCP
	// clients that cannot perform dynamic client registration (an Okta custom
	// authorization server offers no anonymous DCR). Informational only — a
	// client copies it into its own client-registration field. Config-fed
	// (ESHU_AUTH_PREREGISTERED_CLIENT_ID); omitted when unset.
	EshuPreregisteredClientID string `json:"eshu_preregistered_client_id,omitempty"`
}

// OAuthChallengePolicy supplies the per-request RFC 9728/RFC 6750 OAuth
// challenge parameters a 401's WWW-Authenticate header adds (issue #5163,
// F-2): the protected-resource-metadata document URL and the scope string a
// client should request. Implementations must derive "is OAuth enabled"
// from the SAME posture DeriveAuthPosture computes (provider rows + sign-in
// policy) so the challenge and OAuthProtectedResourceHandler's own
// enablement gate never disagree — see PostureOAuthChallengePolicy.
// ok=false (or an empty metadataURL) leaves the challenge exactly the
// pre-#5163 bare "Bearer" — the safe default for a nil policy, a
// posture-derivation error, and a token-only deployment alike. See
// unauthorizedResponse and oauthWWWAuthenticateChallenge in auth.go for the
// one call site this is consumed from, and auth_oauth_challenge_context.go
// for how it reaches that call site without changing unauthorizedResponse's
// signature.
type OAuthChallengePolicy interface {
	OAuthChallenge(ctx context.Context) (metadataURL, scope string, ok bool)
}

// OAuthAuthorizationServerLister lists the issuer URLs currently enabled for
// IdP bearer-token validation — the exact set a token could be routed to and
// accepted for (internal/oidcbearer.Resolver.ActiveIssuers implements this
// structurally; query cannot import oidcbearer directly, since oidcbearer
// already imports query for AuthContext/ScopedTokenResolver, so this
// dependency runs the other way by design, matching the existing
// ScopedTokenResolver interface pattern). A caller with no wired lister (or
// one that returns nothing) yields an empty authorization_servers list, not
// an error — see OAuthProtectedResourceHandler.metadata.
type OAuthAuthorizationServerLister interface {
	ActiveIssuers(ctx context.Context) []string
}

// OAuthProtectedResourceHandler serves the RFC 9728 discovery route (issue
// #5163, F-2). Enablement is derived, not configured: Mount always
// registers the routes, but the handler itself answers 404 unless
// DeriveAuthPosture — the SAME derivation issue #5165 (F-4) uses for the
// login picker — reports at least one configured provider for TenantID AND at
// least one active bearer-token issuer exists. A token-only deployment
// therefore gets exactly today's behavior at this path: 404, indistinguishable
// from the route never having been mounted at all.
//
// TenantID is caller-supplied, not read from a request query parameter: an
// anonymous MCP client fetching this well-known path has no way to name a
// tenant, and unlike the login picker (which serves many tenants from one
// process in a hosted deployment) a self-hosted Eshu install is
// single-tenant, always the fixed pgstatus.BootstrapAdminTenantID
// ("default") every admin/provider config row in that install is seeded and
// created under. Wiring code sets TenantID to that constant.
type OAuthProtectedResourceHandler struct {
	// Providers and Policy feed DeriveAuthPosture exactly like
	// AuthProviderListHandler's identically named fields.
	Providers AuthProviderStore
	Policy    SignInPolicyReadStore
	// TenantID is the fixed tenant this deployment's providers are
	// configured under. See the type doc comment.
	TenantID string
	// Issuers lists the currently active bearer-token issuers for
	// AuthorizationServers. Nil is safe: the document is still served (once
	// a provider is configured) with an empty AuthorizationServers list.
	Issuers OAuthAuthorizationServerLister
	// Resource is the canonical resource identifier (RFC 8707,
	// ESHU_AUTH_RESOURCE_URI). Empty means "IdP bearer validation is not
	// configured at all" (see cmd/api and cmd/mcp-server's
	// newOIDCBearerResolver: this env var unset disables that whole
	// feature), so the route answers 404 even if legacy OIDC/SAML browser
	// login providers exist without bearer-token support wired.
	Resource string
	// ScopesSupported, ResourceName, ResourceDocumentation, and
	// PreregisteredClientID are copied verbatim into the served document when
	// non-empty; see their identically named OAuthProtectedResourceMetadata
	// fields (PreregisteredClientID maps to eshu_preregistered_client_id).
	ScopesSupported       []string
	ResourceName          string
	ResourceDocumentation string
	PreregisteredClientID string
}

// Mount registers the discovery routes. Both a root route and an RFC 9728
// section 3 path-suffixed route are always registered; whether either answers
// 200 or 404 is decided per-request by derived posture (see the type doc
// comment) so operators never need a restart to reflect a provider being added
// or removed.
//
// RFC 9728 section 3 derives a protected resource's metadata URL by inserting
// "/.well-known/oauth-protected-resource" between the host and the path of the
// resource identifier: a resource of https://host/mcp is discovered at
// https://host/.well-known/oauth-protected-resource/mcp. Eshu serves BOTH that
// path-suffixed URL and the bare root (RFC 9728's root fallback), returning the
// identical document, because MCP clients differ in which they query. Any other
// suffix (for example a /sse or /mcp/message transport path) answers 404 so a
// strict client that inserted a different suffix and compares the document's
// "resource" against what it inserted (RFC 9728 section 3.3) falls back to the
// root rather than dead-ending on a canonical document at the wrong suffix.
func (h *OAuthProtectedResourceHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", h.handleMetadataRoot)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/{rest...}", h.handleMetadataSuffix)
}

// handleMetadataRoot serves the bare well-known root. It always attempts to
// serve (subject to the posture gates in serveMetadata) — the root is a valid
// discovery URL for every enabled deployment regardless of the resource path.
func (h *OAuthProtectedResourceHandler) handleMetadataRoot(w http.ResponseWriter, r *http.Request) {
	h.serveMetadata(w, r)
}

// handleMetadataSuffix serves the RFC 9728 section 3 path-suffixed URL, but
// only for the exact, case-sensitive suffix derived from the resource
// identifier's own path. A resource with no path (https://host) has no valid
// suffix, so every suffixed request 404s and only the root serves. Any suffix
// that is not the derived one 404s so strict clients fall back to the root.
func (h *OAuthProtectedResourceHandler) handleMetadataSuffix(w http.ResponseWriter, r *http.Request) {
	if h == nil {
		http.NotFound(w, r)
		return
	}
	expected := metadataResourceSuffix(h.Resource)
	if expected == "" || r.PathValue("rest") != expected {
		http.NotFound(w, r)
		return
	}
	h.serveMetadata(w, r)
}

func (h *OAuthProtectedResourceHandler) serveMetadata(w http.ResponseWriter, r *http.Request) {
	if h == nil || strings.TrimSpace(h.Resource) == "" {
		http.NotFound(w, r)
		return
	}
	posture, err := DeriveAuthPosture(r.Context(), h.Providers, h.Policy, h.TenantID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to derive auth posture")
		return
	}
	if len(posture.Providers) == 0 {
		http.NotFound(w, r)
		return
	}

	var issuers []string
	if h.Issuers != nil {
		issuers = h.Issuers.ActiveIssuers(r.Context())
	}
	if len(issuers) == 0 {
		// §D: a protected-resource document with an empty authorization_servers
		// list is useless — a client cannot learn which issuer to obtain a
		// token from — so answer 404 rather than advertise a resource a client
		// could never complete an OAuth flow for. This also means a deployment
		// whose only providers are browser-login OIDC/SAML (no bearer-token
		// issuer in the active snapshot) is indistinguishable from a token-only
		// stack at this route, which is correct: neither can mint an access
		// token this resource would accept.
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Cache-Control", "public, max-age=60")
	WriteJSON(w, http.StatusOK, OAuthProtectedResourceMetadata{
		Resource:                  h.Resource,
		AuthorizationServers:      issuers,
		BearerMethodsSupported:    []string{"header"},
		ScopesSupported:           h.ScopesSupported,
		ResourceName:              h.ResourceName,
		ResourceDocumentation:     h.ResourceDocumentation,
		EshuPreregisteredClientID: h.PreregisteredClientID,
	})
}

// metadataResourceSuffix returns the RFC 9728 section 3 path suffix for a
// resource identifier: the resource URI's path with surrounding slashes
// trimmed (https://host/mcp -> "mcp", https://host or https://host/ -> ""). An
// unparseable resource yields no suffix, disabling the suffixed route while the
// root route's own Resource-empty and posture gates still apply.
func metadataResourceSuffix(resource string) string {
	resource = strings.TrimSpace(resource)
	if resource == "" {
		return ""
	}
	u, err := url.Parse(resource)
	if err != nil {
		return ""
	}
	return strings.Trim(u.Path, "/")
}

// PostureOAuthChallengePolicy implements OAuthChallengePolicy by deriving
// the SAME AuthPosture OAuthProtectedResourceHandler derives (identical
// Providers/Policy/TenantID), so a 401's WWW-Authenticate challenge points
// at the discovery URL if and only if that exact URL would actually resolve
// — never a challenge pointing a client at a 404.
type PostureOAuthChallengePolicy struct {
	Providers AuthProviderStore
	Policy    SignInPolicyReadStore
	TenantID  string
	// MetadataURL is the absolute (or origin-relative) URL this deployment
	// serves OAuthProtectedResourceHandler at. Empty disables the challenge
	// addition entirely (OAuthChallenge always returns ok=false), matching
	// OAuthProtectedResourceHandler's own Resource-empty 404 gate.
	MetadataURL string
	// Scope is copied verbatim into OAuthChallenge's return value.
	Scope string
}

// OAuthChallenge implements OAuthChallengePolicy.
func (p *PostureOAuthChallengePolicy) OAuthChallenge(ctx context.Context) (metadataURL, scope string, ok bool) {
	if p == nil || strings.TrimSpace(p.MetadataURL) == "" {
		return "", "", false
	}
	posture, err := DeriveAuthPosture(ctx, p.Providers, p.Policy, p.TenantID)
	if err != nil || len(posture.Providers) == 0 {
		// Fail safe to "no challenge addition" on a posture-read error,
		// exactly like DeriveAuthPosture's own sign-in-policy fail-open
		// convention: a transient read failure on the auth-DENIAL hot path
		// must never itself become a second failure mode, and must never
		// mint a challenge pointing at a route that DeriveAuthPosture could
		// not currently prove is enabled.
		return "", "", false
	}
	return p.MetadataURL, p.Scope, true
}
