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
