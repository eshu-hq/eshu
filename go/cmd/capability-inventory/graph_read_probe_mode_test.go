// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestGraphReadProbeSanitizesHostileTransportErrors(t *testing.T) {
	const privateCause = "dial https://private.example.invalid/api/v0/repositories?repo_id=repository:r_private Authorization=Bearer secret-marker"
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New(privateCause)
	})}
	probe := graphReadProbe{
		identity: "api:GET /api/v0/repositories", transport: "api", method: http.MethodGet,
		path: "/api/v0/repositories", auth: graphReadProbeUserAuth, execute: true,
	}
	if err := executeGraphReadProbe(client, "https://private.example.invalid", "https://mcp.private.invalid", "secret-marker", 1, probe); err == nil {
		t.Fatal("executeGraphReadProbe() error = nil, want sanitized transport failure")
	} else {
		assertSanitizedProbeError(t, err, privateCause)
	}
	if _, err := discoverProbeSelectors(client, "https://private.example.invalid", "secret-marker"); err == nil {
		t.Fatal("discoverProbeSelectors() error = nil, want sanitized transport failure")
	} else {
		assertSanitizedProbeError(t, err, privateCause)
	}
}

func assertSanitizedProbeError(t *testing.T, err error, privateCause string) {
	t.Helper()
	for _, forbidden := range []string{privateCause, "private.example.invalid", "/api/v0/repositories", "repository:r_private", "secret-marker", "Bearer"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("probe error exposed %q: %v", forbidden, err)
		}
	}
}

func TestDiscoverProbeSelectorsUsesRepositoryScopedAuthoritativeSources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v0/repositories":
			_, _ = w.Write([]byte(`{"repositories":[{"id":"repo-fixture","name":"repo-name"}]}`))
		case "/api/v0/freshness/generations":
			if request.URL.Query().Get("repository") == "repo-fixture" {
				_, _ = w.Write([]byte(`{"generations":[{"scope_id":"repo-scope","generation_id":"repo-generation"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"generations":[{"scope_id":"state_snapshot:s3:fixture"}]}`))
		case "/api/v0/repositories/repo-fixture/context":
			_, _ = w.Write([]byte(`{"deployment_evidence":{"resolved_id":"relationship-fixture"}}`))
		case "/api/v0/content/entities/search":
			assertRepositoryScopedDiscoveryBody(t, request)
			_, _ = w.Write([]byte(`{"results":[{"entity_id":"entity-fixture"}]}`))
		case "/api/v0/content/files/search":
			assertRepositoryScopedDiscoveryBody(t, request)
			_, _ = w.Write([]byte(`{"results":[{"relative_path":"README.md"}]}`))
		case "/api/v0/service-catalog/correlations":
			if got, want := request.URL.Query().Get("repository_id"), "repo-fixture"; got != want {
				t.Errorf("service-catalog repository_id = %q, want %q", got, want)
			}
			_, _ = w.Write([]byte(`{"correlations":[{"workload_id":"workload-fixture","service_id":"service-fixture","display_name":"service-name"}]}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()

	selectors, err := discoverProbeSelectors(server.Client(), server.URL, "user-token")
	if err != nil {
		t.Fatalf("discoverProbeSelectors() error = %v", err)
	}
	for class, want := range map[string]string{
		"repository_id": "repo-fixture", "repository_name": "repo-name",
		"scope_id": "repo-scope", "generation_id": "repo-generation",
		"terraform_state_scope": "state_snapshot:s3:fixture", "relationship_id": "relationship-fixture",
		"entity_id": "entity-fixture", "relative_path": "README.md", "workload_id": "workload-fixture",
		"service_id": "service-fixture", "service_name": "service-name",
	} {
		if got := selectors[class]; got != want {
			t.Errorf("selector %s = %q, want %q; selectors = %#v", class, got, want, selectors)
		}
	}
}

func assertRepositoryScopedDiscoveryBody(t *testing.T, request *http.Request) {
	t.Helper()
	if request.Method != http.MethodPost {
		t.Errorf("discovery method = %s, want POST", request.Method)
	}
	var body map[string]any
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		t.Errorf("decode discovery body: %v", err)
		return
	}
	if got, want := body["repo_id"], "repo-fixture"; got != want {
		t.Errorf("discovery repo_id = %#v, want %#v", got, want)
	}
}

func TestGraphReadProbeRegistryCoversCurrentDirectSurfaces(t *testing.T) {
	if err := validateGraphReadProbeRegistry(); err != nil {
		t.Fatalf("validateGraphReadProbeRegistry() error = %v", err)
	}
	targets, err := currentAPIAndMCPSurfaces()
	if err != nil {
		t.Fatalf("currentAPIAndMCPSurfaces() error = %v", err)
	}
	if got, want := len(targets), 417; got != want {
		t.Fatalf("current target count = %d, want checked-in current manifest count %d", got, want)
	}
}

func TestCurrentProbeRegistryClassifiesEverySurfaceWithoutGenericUnsupported(t *testing.T) {
	registry, err := buildCurrentProbeRegistry()
	if err != nil {
		t.Fatalf("buildCurrentProbeRegistry() error = %v", err)
	}
	if got, want := len(registry), 417; got != want {
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

func TestCurrentProbeRegistrySynthesizesAWSRuntimeDriftAnyOfScope(t *testing.T) {
	registry, err := buildCurrentProbeRegistry()
	if err != nil {
		t.Fatalf("buildCurrentProbeRegistry() error = %v", err)
	}

	probe := requireProbeIdentity(t, registry, "api:POST /api/v0/aws/runtime-drift/findings")
	if got, want := probe.arguments["scope_id"], selector("scope_id"); got != want {
		t.Fatalf("AWS runtime-drift scope_id = %#v, want %#v; arguments = %#v", got, want, probe.arguments)
	}
	if _, ok := probe.arguments["account_id"]; ok {
		t.Fatalf("AWS runtime-drift arguments = %#v, want deterministic first anyOf branch only", probe.arguments)
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
		"entity_id": {}, "cloud_resource_id": {}, "arn": {}, "search_term": {}, "relative_path": {}, "scope_id": {},
		"generation_id": {}, "terraform_state_scope": {}, "package_id": {}, "fact_kind": {}, "collector_family": {},
		"component_id": {}, "finding_id": {}, "packet_id": {}, "incident_id": {}, "relationship_id": {},
		"workflow_id": {}, "playbook_id": {}, "tag": {}, "oci_repository_id": {},
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

func TestCurrentProbeRegistryResolvesEveryExecutableFixture(t *testing.T) {
	registry, err := buildCurrentProbeRegistry()
	if err != nil {
		t.Fatalf("buildCurrentProbeRegistry() error = %v", err)
	}
	selectors := map[string]string{
		"repository_id": "repo-fixture", "repository_name": "repo-name",
		"service_id": "service-fixture", "service_name": "service-name", "workload_id": "workload-fixture",
		"entity_id": "entity-fixture", "cloud_resource_id": "cloud-fixture",
		"arn": "arn:aws:lambda:us-east-1:123456789012:function:fixture", "search_term": "main",
		"relative_path": "README.md", "scope_id": "repo-scope-fixture", "generation_id": "generation-fixture",
		"terraform_state_scope": "state_snapshot:s3:fixture", "package_id": "package-fixture",
		"fact_kind": "repository", "collector_family": "repository", "component_id": "component-fixture",
		"finding_id": "finding-fixture", "packet_id": "packet-fixture", "relationship_id": "relationship-fixture",
		"workflow_id": "workflow-fixture", "playbook_id": "playbook-fixture", "tag": "tag-fixture",
		"oci_repository_id": "oci-registry://ghcr.io/eshu-hq/demo",
	}
	classified := 0
	for _, probe := range registry {
		classified++
		if !probe.execute {
			continue
		}
		if _, err := resolveProbeSelectors(probe, selectors); err != nil {
			t.Errorf("resolveProbeSelectors(%s) error = %v", probe.identity, err)
		}
	}
	if got, want := classified, 417; got != want {
		t.Fatalf("classified registry entries = %d, want %d", got, want)
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

// probeFixtureGraphReader is a minimal query.GraphQuery fake used only to let
// the real TagHistoryHandler reach its success path; it asserts nothing about
// query.GraphQuery is required to correctly reject/accept repository_id
// shape, which happens before Run is ever called.
type probeFixtureGraphReader struct{}

func (probeFixtureGraphReader) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (probeFixtureGraphReader) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

// TestGraphReadProbeTagHistoryFixtureMatchesRealHandlerContract mounts the
// actual query.TagHistoryHandler -- not a generic always-200 stub -- to prove
// the tag-history probe fixture satisfies the real repository_id contract.
//
// go/internal/query/tag_history.go composeOCIImageRef requires repository_id
// to carry the oci-registry:// prefix and 400s otherwise (proven here on a
// git-shaped id, the same shape the "repository_id" selector class discovers
// from GET /api/v0/repositories). The checked-in fixture must instead resolve
// through the dedicated "oci_repository_id" selector class so it is accepted.
func TestGraphReadProbeTagHistoryFixtureMatchesRealHandlerContract(t *testing.T) {
	registry, err := buildCurrentProbeRegistry()
	if err != nil {
		t.Fatalf("buildCurrentProbeRegistry() error = %v", err)
	}
	probe := requireProbeIdentity(t, registry, "api:GET /api/v0/images/tag-history")

	handler := &query.TagHistoryHandler{Neo4j: probeFixtureGraphReader{}, Profile: query.ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	// RED: the git-shaped repository_id the discovered "repository_id"
	// selector class would have supplied (what the fixture sent before this
	// fix) is rejected by the real handler.
	gitShaped := httptest.NewRecorder()
	mux.ServeHTTP(gitShaped, httptest.NewRequest(
		http.MethodGet, "/api/v0/images/tag-history?repository_id=repository%3Ar_fixture&tag=latest&limit=1", nil,
	))
	if got, want := gitShaped.Code, http.StatusBadRequest; got != want {
		t.Fatalf("git-shaped repository_id status = %d, want %d (the old fixture shape must still 400); body = %s",
			got, want, gitShaped.Body.String())
	}

	// GREEN: the checked-in fixture, resolved through the real
	// oci_repository_id selector, is accepted by the real handler.
	selectors := map[string]string{
		"oci_repository_id": "oci-registry://ghcr.io/eshu-hq/demo", "tag": "latest",
	}
	resolved, err := resolveProbeSelectors(probe, selectors)
	if err != nil {
		t.Fatalf("resolveProbeSelectors() error = %v", err)
	}
	values := url.Values{}
	for name, value := range resolved.query {
		values.Set(name, fmt.Sprint(value))
	}
	fixture := httptest.NewRecorder()
	mux.ServeHTTP(fixture, httptest.NewRequest(http.MethodGet, resolved.path+"?"+values.Encode(), nil))
	if got, want := fixture.Code, http.StatusOK; got != want {
		t.Fatalf("resolved tag-history fixture status = %d, want %d; body = %s", got, want, fixture.Body.String())
	}
}
