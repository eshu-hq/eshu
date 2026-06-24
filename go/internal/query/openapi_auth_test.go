package query

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenAPIIncludesBrowserSessionRoutes(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	paths := mustMapField(t, spec, "paths")
	loginPath := mustMapField(t, paths, "/api/v0/auth/oidc/login")
	login := mustMapField(t, loginPath, "get")
	loginDescription, ok := login["description"].(string)
	if !ok {
		t.Fatal("OIDC login GET description missing")
	}
	for _, want := range []string{
		"Authorization Code",
		"state",
		"nonce",
		"Eshu roles and grants",
		"raw OIDC tokens",
	} {
		if !strings.Contains(loginDescription, want) {
			t.Fatalf("OIDC login GET description missing %q: %s", want, loginDescription)
		}
	}

	callbackPath := mustMapField(t, paths, "/api/v0/auth/oidc/callback")
	callback := mustMapField(t, callbackPath, "get")
	callbackDescription, ok := callback["description"].(string)
	if !ok {
		t.Fatal("OIDC callback GET description missing")
	}
	for _, want := range []string{
		"issuer metadata/JWKS",
		"hashed external groups",
		"browser-session cookies",
		"create no session",
	} {
		if !strings.Contains(callbackDescription, want) {
			t.Fatalf("OIDC callback GET description missing %q: %s", want, callbackDescription)
		}
	}

	sessionPath := mustMapField(t, paths, "/api/v0/auth/browser-session")
	create := mustMapField(t, sessionPath, "post")
	createDescription, ok := create["description"].(string)
	if !ok {
		t.Fatal("browser session POST description missing")
	}
	for _, want := range []string{
		"__Host-eshu_session",
		"HttpOnly",
		"Secure",
		"SameSite=Strict",
		"X-Eshu-CSRF",
		"server persists only SHA-256 hashes",
	} {
		if !strings.Contains(createDescription, want) {
			t.Fatalf("browser session POST description missing %q: %s", want, createDescription)
		}
	}

	contextPath := mustMapField(t, paths, "/api/v0/auth/browser-session/context")
	switchRoute := mustMapField(t, contextPath, "patch")
	switchDescription, ok := switchRoute["description"].(string)
	if !ok {
		t.Fatal("browser session context PATCH description missing")
	}
	if !strings.Contains(switchDescription, "X-Eshu-CSRF") {
		t.Fatalf("browser session switch description missing CSRF header: %s", switchDescription)
	}

	components := mustMapField(t, spec, "components")
	schemas := mustMapField(t, components, "schemas")
	if _, ok := schemas["BrowserSessionResponse"]; !ok {
		t.Fatal("BrowserSessionResponse schema missing")
	}
	sessionAuth := mustMapField(t, schemas, "BrowserSessionAuth")
	sessionAuthProperties := mustMapField(t, sessionAuth, "properties")
	if _, ok := sessionAuthProperties["role_ids"]; !ok {
		t.Fatal("BrowserSessionAuth role_ids schema missing")
	}
	if _, ok := sessionAuthProperties["permission_catalog_enforced"]; !ok {
		t.Fatal("BrowserSessionAuth permission_catalog_enforced schema missing")
	}
	if _, ok := sessionAuthProperties["allowed_permission_features"]; !ok {
		t.Fatal("BrowserSessionAuth allowed_permission_features schema missing")
	}
	responses := mustMapField(t, components, "responses")
	if _, ok := responses["Unauthorized"]; !ok {
		t.Fatal("Unauthorized response component missing")
	}
}

func TestBrowserSessionAuthResponseSerializesPermissionCatalogFields(t *testing.T) {
	auth := AuthContext{
		Mode:                      AuthModeBrowserSession,
		TenantID:                  "tenant_a",
		WorkspaceID:               "ws_a",
		AllScopes:                 false,
		PermissionCatalogEnforced: true,
		AllowedPermissionFeatures: []string{"ask_search", "repository_content"},
	}
	resp := browserSessionAuthResponse(auth)
	if !resp.PermissionCatalogEnforced {
		t.Fatal("expected PermissionCatalogEnforced=true")
	}
	if len(resp.AllowedPermissionFeatures) != 2 {
		t.Fatalf("expected 2 AllowedPermissionFeatures, got %d", len(resp.AllowedPermissionFeatures))
	}
	if resp.AllowedPermissionFeatures[0] != "ask_search" || resp.AllowedPermissionFeatures[1] != "repository_content" {
		t.Fatalf("unexpected AllowedPermissionFeatures: %v", resp.AllowedPermissionFeatures)
	}

	// Verify the response with all_scopes=true produces permission_catalog_enforced=false (zero value).
	allScopesAuth := AuthContext{
		Mode:                      AuthModeBrowserSession,
		AllScopes:                 true,
		PermissionCatalogEnforced: false,
	}
	allScopesResp := browserSessionAuthResponse(allScopesAuth)
	if allScopesResp.PermissionCatalogEnforced {
		t.Fatal("expected PermissionCatalogEnforced=false for all_scopes session")
	}
	if len(allScopesResp.AllowedPermissionFeatures) != 0 {
		t.Fatalf("expected empty AllowedPermissionFeatures for all_scopes session, got %v", allScopesResp.AllowedPermissionFeatures)
	}
}

func TestOpenAPIIncludesLocalIdentityRoutes(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	paths := mustMapField(t, spec, "paths")
	for _, path := range []string{
		"/api/v0/auth/local/bootstrap",
		"/api/v0/auth/local/login",
		"/api/v0/auth/local/invitations",
		"/api/v0/auth/local/invitations/accept",
		"/api/v0/auth/local/users/{user_id}/password",
		"/api/v0/auth/local/users/{user_id}/mfa-reset",
		"/api/v0/auth/local/users/{user_id}/disable",
		"/api/v0/auth/local/api-tokens",
		"/api/v0/auth/local/api-tokens/{token_id}/revoke",
		"/api/v0/auth/local/api-tokens/{token_id}/rotate",
		"/api/v0/auth/local/break-glass",
		"/api/v0/auth/local/break-glass/session",
	} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("OpenAPI path %q missing", path)
		}
	}

	bootstrap := mustMapField(t, mustMapField(t, paths, "/api/v0/auth/local/bootstrap"), "post")
	bootstrapDescription, ok := bootstrap["description"].(string)
	if !ok || !strings.Contains(bootstrapDescription, "requires the shared operator bearer token") ||
		!strings.Contains(bootstrapDescription, "MFA") {
		t.Fatalf("bootstrap description missing operator/MFA contract: %v", bootstrap["description"])
	}
	login := mustMapField(t, mustMapField(t, paths, "/api/v0/auth/local/login"), "post")
	loginDescription, ok := login["description"].(string)
	if !ok || !strings.Contains(loginDescription, "Public local-login route") ||
		!strings.Contains(loginDescription, "lockout") {
		t.Fatalf("login description missing public/lockout contract: %v", login["description"])
	}
	breakGlass := mustMapField(t, mustMapField(t, paths, "/api/v0/auth/local/break-glass"), "post")
	breakGlassDescription, ok := breakGlass["description"].(string)
	if !ok || !strings.Contains(breakGlassDescription, "disabled by default") ||
		!strings.Contains(breakGlassDescription, "stores only a break-glass code hash") {
		t.Fatalf("break-glass description missing safety contract: %v", breakGlass["description"])
	}
	apiTokens := mustMapField(t, mustMapField(t, paths, "/api/v0/auth/local/api-tokens"), "post")
	apiTokenDescription, ok := apiTokens["description"].(string)
	if !ok || !strings.Contains(apiTokenDescription, "returned once") ||
		!strings.Contains(apiTokenDescription, "storage persists only token_hash") {
		t.Fatalf("api token description missing one-time/hash-only contract: %v", apiTokens["description"])
	}

	components := mustMapField(t, spec, "components")
	schemas := mustMapField(t, components, "schemas")
	for _, schema := range []string{
		"LocalIdentityBootstrapRequest",
		"LocalIdentityLoginRequest",
		"LocalIdentitySessionResponse",
		"LocalIdentityAPITokenCreateRequest",
		"LocalIdentityAPITokenResponse",
	} {
		if _, ok := schemas[schema]; !ok {
			t.Fatalf("schema %q missing", schema)
		}
	}
}

func TestOpenAPIIncludesSAMLRoutes(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	paths := mustMapField(t, spec, "paths")
	metadataPath := mustMapField(t, paths, "/api/v0/auth/saml/providers/{provider_id}/metadata")
	metadata := mustMapField(t, metadataPath, "get")
	metadataDescription, ok := metadata["description"].(string)
	if !ok {
		t.Fatal("SAML metadata description missing")
	}
	for _, want := range []string{
		"public",
		"service-provider metadata",
		"raw assertions",
	} {
		if !strings.Contains(metadataDescription, want) {
			t.Fatalf("SAML metadata description missing %q: %s", want, metadataDescription)
		}
	}

	loginPath := mustMapField(t, paths, "/api/v0/auth/saml/providers/{provider_id}/login")
	login := mustMapField(t, loginPath, "get")
	loginDescription, ok := login["description"].(string)
	if !ok {
		t.Fatal("SAML login description missing")
	}
	for _, want := range []string{
		"RelayState",
		"SHA-256 hash",
		"AuthnRequest",
	} {
		if !strings.Contains(loginDescription, want) {
			t.Fatalf("SAML login description missing %q: %s", want, loginDescription)
		}
	}

	acsPath := mustMapField(t, paths, "/api/v0/auth/saml/providers/{provider_id}/acs")
	acs := mustMapField(t, acsPath, "post")
	acsDescription, ok := acs["description"].(string)
	if !ok {
		t.Fatal("SAML ACS description missing")
	}
	for _, want := range []string{
		"RelayState",
		"SAMLResponse",
		"replay",
		"__Host-eshu_session",
		"server persists only SHA-256 hashes",
	} {
		if !strings.Contains(acsDescription, want) {
			t.Fatalf("SAML ACS description missing %q: %s", want, acsDescription)
		}
	}
}
