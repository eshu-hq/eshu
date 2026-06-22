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
	responses := mustMapField(t, components, "responses")
	if _, ok := responses["Unauthorized"]; !ok {
		t.Fatal("Unauthorized response component missing")
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

	components := mustMapField(t, spec, "components")
	schemas := mustMapField(t, components, "schemas")
	for _, schema := range []string{
		"LocalIdentityBootstrapRequest",
		"LocalIdentityLoginRequest",
		"LocalIdentitySessionResponse",
	} {
		if _, ok := schemas[schema]; !ok {
			t.Fatalf("schema %q missing", schema)
		}
	}
}
