// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestGraphReadProbeRegistryCoversCurrentDirectSurfaces(t *testing.T) {
	if err := validateGraphReadProbeRegistry(); err != nil {
		t.Fatalf("validateGraphReadProbeRegistry() error = %v", err)
	}
	targets, err := currentAPIAndMCPSurfaces()
	if err != nil {
		t.Fatalf("currentAPIAndMCPSurfaces() error = %v", err)
	}
	if got, want := len(targets), 415; got != want {
		t.Fatalf("current target count = %d, want checked-in current manifest count %d", got, want)
	}
}

func TestCurrentProbeRegistryClassifiesEverySurfaceWithoutGenericUnsupported(t *testing.T) {
	registry, err := buildCurrentProbeRegistry()
	if err != nil {
		t.Fatalf("buildCurrentProbeRegistry() error = %v", err)
	}
	if got, want := len(registry), 415; got != want {
		t.Fatalf("registry count = %d, want %d", got, want)
	}
	seen := map[string]struct{}{}
	for _, probe := range registry {
		if _, ok := seen[probe.identity]; ok {
			t.Fatalf("duplicate registry identity %q", probe.identity)
		}
		seen[probe.identity] = struct{}{}
		if !probe.execute && (probe.unsafeReason == "" || strings.Contains(probe.unsafeReason, "unsupported")) {
			t.Fatalf("probe %q execute=false reason = %q, want specific unsafe mutation reason", probe.identity, probe.unsafeReason)
		}
	}
}

func TestCurrentProbeRegistryIsRedactedAndClassifiesMappedDelta(t *testing.T) {
	registry, err := buildCurrentProbeRegistry()
	if err != nil {
		t.Fatalf("buildCurrentProbeRegistry() error = %v", err)
	}
	text := fmt.Sprintf("%#v", registry)
	for _, forbidden := range []string{
		"localhost",
		"/Users/",
		"repository:r_",
		"content-entity:e_",
		"workload:android",
		"Bearer ",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("registry contains retained value %q", forbidden)
		}
	}
	allowedSelectors := map[string]struct{}{
		"repository_id": {}, "repository_name": {}, "service_id": {}, "service_name": {}, "workload_id": {},
		"entity_id": {}, "cloud_resource_id": {}, "search_term": {}, "relative_path": {}, "scope_id": {},
		"generation_id": {}, "terraform_state_scope": {}, "package_id": {}, "fact_kind": {}, "collector_family": {},
		"component_id": {}, "finding_id": {}, "packet_id": {}, "incident_id": {}, "relationship_id": {},
		"workflow_id": {}, "playbook_id": {},
	}
	unknownSelectors := map[string]struct{}{}
	for _, match := range regexp.MustCompile(`\{\{selector:([^}]+)}}`).FindAllStringSubmatch(text, -1) {
		if _, ok := allowedSelectors[match[1]]; !ok {
			unknownSelectors[match[1]] = struct{}{}
		}
	}
	if len(unknownSelectors) > 0 {
		t.Fatalf("registry contains undeclared selector classes: %v", unknownSelectors)
	}
	for _, identity := range []string{
		"api:GET /api/v0/auth/github/login",
		"api:GET /api/v0/auth/github/callback",
		"api:GET /api/v0/codeowners/ownership",
		"api:GET /api/v0/replatforming/selectors",
		"api:GET /api/v0/services/{service_name}/intelligence-report",
		"api:POST /api/v0/terraform/config-state-drift/findings",
		"mcp:list_codeowners_ownership",
		"mcp:list_terraform_config_state_drift_findings",
	} {
		probe := requireProbeIdentity(t, registry, identity)
		if !probe.execute {
			t.Fatalf("mapped delta %q execute=false: %s", identity, probe.unsafeReason)
		}
	}
	if probe := requireProbeIdentity(t, registry, "api:POST /api/v0/admin/reindex"); probe.execute || !strings.Contains(probe.unsafeReason, "reindex") {
		t.Fatalf("admin reindex classification = %#v, want explicit unsafe mutation", probe)
	}
	for _, test := range []struct {
		identity string
		execute  bool
		auth     graphReadProbeAuth
	}{
		{"api:DELETE /api/v0/auth/browser-session", false, graphReadProbeUserAuth},
		{"api:POST /api/v0/admin/work-items/query", true, graphReadProbeAdminAuth},
		{"api:POST /api/v0/code/cypher", true, graphReadProbeAdminAuth},
		{"api:GET /api/v0/auth/github/login", true, "public"},
		{"api:GET /api/v0/repositories", true, graphReadProbeUserAuth},
		{"mcp:execute_cypher_query", true, graphReadProbeAdminAuth},
		{"mcp:list_indexed_repositories", true, graphReadProbeUserAuth},
	} {
		probe := requireProbeIdentity(t, registry, test.identity)
		if probe.execute != test.execute || probe.auth != test.auth {
			t.Fatalf("classification %q = execute %t auth %q, want %t/%q", test.identity, probe.execute, probe.auth, test.execute, test.auth)
		}
	}
}

func requireProbeIdentity(t *testing.T, registry []graphReadProbe, identity string) graphReadProbe {
	t.Helper()
	for _, probe := range registry {
		if probe.identity == identity {
			return probe
		}
	}
	t.Fatalf("registry missing %q", identity)
	return graphReadProbe{}
}

func TestRunGraphReadProbeUsesDeclaredArgumentsAuthAndExplicitSelectorFailure(t *testing.T) {
	const userToken = "user-secret"
	const adminToken = "admin-secret"
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		wantToken := userToken
		if request.URL.Path == "/health" || request.URL.Path == "/api/v0/openapi.json" ||
			request.URL.Path == "/api/v0/docs" || request.URL.Path == "/api/v0/redoc" ||
			request.URL.Path == "/api/v0/auth/setup-state" || strings.HasPrefix(request.URL.Path, "/api/v0/auth/github/") {
			wantToken = ""
		}
		if strings.HasPrefix(request.URL.Path, "/api/v0/admin/") {
			wantToken = adminToken
		}
		if request.URL.Path == "/api/v0/code/cypher" || request.URL.Path == "/api/v0/code/visualize" {
			wantToken = adminToken
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Errorf("decode API body: %v", err)
			}
			if body["cypher_query"] != graphReadProbeCypher || body["limit"] != float64(1) {
				t.Errorf("API body = %#v, want checked-in query and limit", body)
			}
		}
		wantHeader := ""
		if wantToken != "" {
			wantHeader = "Bearer " + wantToken
		}
		if request.Header.Get("Authorization") != wantHeader {
			t.Errorf("API auth = %q, want bearer token for declared posture", request.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"repositories":[{"id":"repo-fixture","name":"repo-name","scope_id":"scope-fixture","generation_id":"generation-fixture"}]}`))
	}))
	defer api.Close()

	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var call struct {
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Errorf("decode MCP call: %v", err)
		}
		wantToken := userToken
		if strings.Contains(call.Params.Name, "cypher") || call.Params.Name == "visualize_graph_query" {
			wantToken = adminToken
			if call.Params.Arguments["cypher_query"] != graphReadProbeCypher {
				t.Errorf("MCP arguments = %#v, want checked-in query", call.Params.Arguments)
			}
		}
		if request.Header.Get("Authorization") != "Bearer "+wantToken {
			t.Errorf("MCP auth = %q, want bearer token for declared posture", request.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`))
	}))
	defer mcp.Close()

	var output strings.Builder
	err := runGraphReadProbe(&output, api.Client(), api.URL, mcp.URL, userToken, adminToken)
	if err == nil || !strings.Contains(err.Error(), "missing discovered selector class") {
		t.Fatalf("runGraphReadProbe() error = %v, want explicit missing-selector failure", err)
	}
	if strings.Contains(err.Error(), "current surfaces lack checked-in fixtures") {
		t.Fatalf("runGraphReadProbe() retained generic unsupported bucket: %v", err)
	}
	if !strings.Contains(output.String(), "PASS") || !strings.Contains(output.String(), "SKIP") {
		t.Fatalf("output = %q, want classified PASS and mutation SKIP rows", output.String())
	}
}

func TestRunGraphReadProbeFailsClosedOnUnsupportedSurface(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	err := runGraphReadProbe(&strings.Builder{}, server.Client(), server.URL, server.URL, "user", "admin")
	if err == nil || !strings.Contains(err.Error(), "bounded selector discovery HTTP status 404") {
		t.Fatalf("runGraphReadProbe() error = %v, want explicit selector-discovery failure", err)
	}
}
