// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
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
	if _, err := discoverProbeSelectors(client, "https://private.example.invalid", "secret-marker", "secret-marker"); err == nil {
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

	selectors, err := discoverProbeSelectors(server.Client(), server.URL, "user-token", "admin-token")
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

// probeRegistryStubSpec is the canned satellite OpenAPI document the
// registry tests build against: every asserted identity plus the anyOf
// scope shape the AWS runtime-drift fixture test pins. Probes run against
// the satellite, so the registry tests run against a stub satellite rather
// than in-tree packages.
const probeRegistryStubSpec = `{
  "openapi": "3.0.0",
  "paths": {
    "/api/v0/repositories": {"get": {"responses": {"200": {"description": "ok"}}}},
    "/api/v0/code/cypher": {"post": {"requestBody": {"content": {"application/json": {"schema": {"type": "object", "properties": {"cypher_query": {"type": "string"}}, "required": ["cypher_query"]}}}}, "responses": {"200": {"description": "ok"}}}},
    "/api/v0/aws/runtime-drift/findings": {"post": {"requestBody": {"content": {"application/json": {"schema": {"type": "object", "properties": {"scope_id": {"type": "string"}, "account_id": {"type": "string"}}, "anyOf": [{"required": ["scope_id"]}, {"required": ["account_id"]}]}}}}, "responses": {"200": {"description": "ok"}}}},
    "/api/v0/auth/github/login": {"get": {"responses": {"200": {"description": "ok"}}}},
    "/api/v0/auth/github/callback": {"get": {"responses": {"200": {"description": "ok"}}}},
    "/api/v0/codeowners/ownership": {"get": {"responses": {"200": {"description": "ok"}}}},
    "/api/v0/replatforming/selectors": {"get": {"responses": {"200": {"description": "ok"}}}},
    "/api/v0/services/{service_name}/intelligence-report": {"get": {"responses": {"200": {"description": "ok"}}}},
    "/api/v0/terraform/config-state-drift/findings": {"post": {"responses": {"200": {"description": "ok"}}}},
    "/api/v0/admin/reindex": {"post": {"responses": {"200": {"description": "ok"}}}},
    "/api/v0/supply-chain/impact/suppressions": {"post": {"responses": {"200": {"description": "ok"}}}},
    "/api/v0/auth/browser-session": {"delete": {"responses": {"200": {"description": "ok"}}}},
    "/api/v0/answer-quality/score": {"post": {"responses": {"200": {"description": "ok"}}}},
    "/api/v0/investigation/packet/render": {"post": {"responses": {"200": {"description": "ok"}}}},
    "/api/v0/report/bundle/capture": {"post": {"responses": {"200": {"description": "ok"}}}},
    "/api/v0/report/bundle/validate": {"post": {"responses": {"200": {"description": "ok"}}}},
    "/api/v0/service/report/compose": {"post": {"responses": {"200": {"description": "ok"}}}},
    "/api/v0/admin/work-items/query": {"post": {"responses": {"200": {"description": "ok"}}}},
    "/api/v0/images/tag-history": {"get": {"responses": {"200": {"description": "ok"}}}}
  }
}`

// probeRegistryStubTools is the canned satellite tools/list payload: every
// MCP tool the registry tests assert on.
const probeRegistryStubTools = `{"jsonrpc":"2.0","id":1,"result":{"tools":[
  {"name":"list_indexed_repositories","inputSchema":{"type":"object"}},
  {"name":"get_repository_stats","inputSchema":{"type":"object"}},
  {"name":"execute_cypher_query","inputSchema":{"type":"object"}},
  {"name":"visualize_graph_query","inputSchema":{"type":"object"}},
  {"name":"list_codeowners_ownership","inputSchema":{"type":"object"}},
  {"name":"list_terraform_config_state_drift_findings","inputSchema":{"type":"object"}},
  {"name":"investigate_code_divergence","inputSchema":{"type":"object"}}
]}}`

// newProbeRegistrySatellite serves the canned eshu-api surface the registry
// tests build against: the OpenAPI document at /api/v0/openapi.json and the
// MCP tools/list payload for every POST. One server plays both roles because
// the two fetches key on path and method, never on host.
func newProbeRegistrySatellite(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/api/v0/openapi.json" {
			_, _ = w.Write([]byte(probeRegistryStubSpec))
			return
		}
		if request.Method == http.MethodPost {
			_, _ = w.Write([]byte(probeRegistryStubTools))
			return
		}
		t.Errorf("stub satellite got unexpected %s %s", request.Method, request.URL.Path)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)
	return server
}

// probeRegistrySatelliteClient builds the registry against the stub
// satellite: the same fetch path production uses, with no in-tree packages.
func probeRegistrySatelliteClient(t *testing.T) (*http.Client, string, string) {
	t.Helper()
	server := newProbeRegistrySatellite(t)
	return server.Client(), server.URL, server.URL
}

func TestValidateGraphReadProbeRegistryAcceptsCurrentSurfaces(t *testing.T) {
	client, apiURL, mcpURL := probeRegistrySatelliteClient(t)
	if err := validateGraphReadProbeRegistry(client, apiURL, mcpURL, "user-token"); err != nil {
		t.Fatalf("validateGraphReadProbeRegistry() error = %v", err)
	}
}

func TestValidateGraphReadProbeRegistryFailsClosedOnMissingSurface(t *testing.T) {
	thin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/api/v0/openapi.json" {
			_, _ = w.Write([]byte(`{"openapi":"3.0.0","paths":{"/api/v0/repositories":{"get":{}}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`))
	}))
	defer thin.Close()

	err := validateGraphReadProbeRegistry(thin.Client(), thin.URL, thin.URL, "user-token")
	if err == nil || !strings.Contains(err.Error(), "the satellite does not serve") {
		t.Fatalf("validateGraphReadProbeRegistry() error = %v, want fail-closed on the missing cypher surface", err)
	}
}

func TestCurrentProbeRegistryClassifiesEverySurfaceWithoutGenericUnsupported(t *testing.T) {
	client, apiURL, mcpURL := probeRegistrySatelliteClient(t)
	registry, err := buildCurrentProbeRegistry(client, apiURL, mcpURL, "user-token")
	if err != nil {
		t.Fatalf("buildCurrentProbeRegistry() error = %v", err)
	}
	if len(registry) == 0 {
		t.Fatal("buildCurrentProbeRegistry() returned no probes")
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
	client, apiURL, mcpURL := probeRegistrySatelliteClient(t)
	registry, err := buildCurrentProbeRegistry(client, apiURL, mcpURL, "user-token")
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
	client, apiURL, mcpURL := probeRegistrySatelliteClient(t)
	registry, err := buildCurrentProbeRegistry(client, apiURL, mcpURL, "user-token")
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
		"component_id": {}, "finding_id": {}, "fingerprint": {}, "packet_id": {}, "incident_id": {}, "relationship_id": {},
		"workflow_id": {}, "playbook_id": {}, "tag": {}, "oci_repository_id": {},
		"collector_kind": {}, "instance_id": {}, "package": {}, "directive": {},
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
	if probe := requireProbeIdentity(t, registry, "api:POST /api/v0/supply-chain/impact/suppressions"); probe.execute ||
		!strings.Contains(probe.unsafeReason, "suppression") {
		t.Fatalf("suppression mutation classification = %#v, want explicit unsafe mutation", probe)
	}
	for _, test := range []struct {
		identity string
		execute  bool
		auth     graphReadProbeAuth
	}{
		{"api:DELETE /api/v0/auth/browser-session", false, graphReadProbeUserAuth},
		{"api:POST /api/v0/answer-quality/score", false, graphReadProbeUserAuth},
		{"api:POST /api/v0/investigation/packet/render", false, graphReadProbeUserAuth},
		{"api:POST /api/v0/report/bundle/capture", false, graphReadProbeUserAuth},
		{"api:POST /api/v0/report/bundle/validate", false, graphReadProbeUserAuth},
		{"api:POST /api/v0/service/report/compose", false, graphReadProbeUserAuth},
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

// probeRunStubSpec is the small satellite surface the declared-arguments run
// test executes against: two executable reads, one executable MCP-free write
// path for SKIP coverage, and nothing the stub discovery can fully resolve
// (the divergence fingerprint stays undiscovered, failing the run closed).
const probeRunStubSpec = `{
  "openapi": "3.0.0",
  "paths": {
    "/api/v0/repositories": {"get": {"responses": {"200": {"description": "ok"}}}},
    "/api/v0/code/cypher": {"post": {"requestBody": {"content": {"application/json": {"schema": {"type": "object", "properties": {"cypher_query": {"type": "string"}}, "required": ["cypher_query"]}}}}, "responses": {"200": {"description": "ok"}}}},
    "/api/v0/admin/reindex": {"post": {"responses": {"200": {"description": "ok"}}}}
  }
}`

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
		if request.URL.Path == "/api/v0/openapi.json" {
			wantHeader := ""
			if request.Header.Get("Authorization") != wantHeader {
				t.Errorf("API auth = %q, want no Authorization for the public doc route", request.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(probeRunStubSpec))
			return
		}
		// Collector selector discovery reads with the admin token.
		if strings.HasPrefix(request.URL.Path, "/api/v0/admin/") || request.URL.Path == "/api/v0/status/collectors" {
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
		var envelope struct {
			Method string `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&envelope); err != nil {
			t.Errorf("decode MCP call: %v", err)
		}
		if envelope.Method == "tools/list" {
			if request.Header.Get("Authorization") != "Bearer "+userToken {
				t.Errorf("MCP tools/list auth = %q, want user bearer", request.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(probeRegistryStubTools))
			return
		}
		call := envelope.Params
		wantToken := userToken
		if strings.Contains(call.Name, "cypher") || call.Name == "visualize_graph_query" {
			wantToken = adminToken
			if call.Arguments["cypher_query"] != graphReadProbeCypher {
				t.Errorf("MCP arguments = %#v, want checked-in query", call.Arguments)
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
	if !strings.Contains(err.Error(), "surface mcp:investigate_code_divergence fixture:") {
		t.Fatalf("runGraphReadProbe() error = %v, want the failing surface named, not a generic bucket", err)
	}
	if !strings.Contains(output.String(), "PASS") || !strings.Contains(output.String(), "SKIP") {
		t.Fatalf("output = %q, want classified PASS and mutation SKIP rows", output.String())
	}
}

func TestCurrentProbeRegistryResolvesEveryExecutableFixture(t *testing.T) {
	client, apiURL, mcpURL := probeRegistrySatelliteClient(t)
	registry, err := buildCurrentProbeRegistry(client, apiURL, mcpURL, "user-token")
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
		"finding_id": "finding-fixture", "fingerprint": "fingerprint-fixture", "packet_id": "packet-fixture", "relationship_id": "relationship-fixture",
		"workflow_id": "workflow-fixture", "playbook_id": "playbook-fixture", "tag": "tag-fixture",
		"oci_repository_id": "oci-registry://ghcr.io/eshu-hq/demo", "collector_kind": "git", "instance_id": "git-main",
		"package": "package-fixture", "directive": "memory_limit",
	}
	for _, probe := range registry {
		if !probe.execute {
			continue
		}
		if _, err := resolveProbeSelectors(probe, selectors); err != nil {
			t.Errorf("resolveProbeSelectors(%s) error = %v", probe.identity, err)
		}
	}
}

func TestRunGraphReadProbeFailsClosedOnUnsupportedSurface(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	err := runGraphReadProbe(&strings.Builder{}, server.Client(), server.URL, server.URL, "user", "admin")
	if err == nil || !strings.Contains(err.Error(), "satellite openapi HTTP status 404") {
		t.Fatalf("runGraphReadProbe() error = %v, want explicit satellite-fetch failure", err)
	}
}

// TestGraphReadProbeTagHistoryFixtureUsesOCIRegistrySelector proves the
// tag-history probe fixture resolves through the dedicated
// "oci_repository_id" selector class instead of the discovered,
// git-shaped "repository_id" class: the served tag-history read contract
// requires the oci-registry:// prefix and 400s otherwise, so a fixture
// carrying the git shape would fail against the satellite.
//
// The 400 itself is pinned by the serving side's own contract tests; this
// test pins the fixture side -- what this command sends. It runs against
// the stub satellite, not in-tree packages (Refs eshu-hq/eshu#9669, row 7).
func TestGraphReadProbeTagHistoryFixtureUsesOCIRegistrySelector(t *testing.T) {
	client, apiURL, mcpURL := probeRegistrySatelliteClient(t)
	registry, err := buildCurrentProbeRegistry(client, apiURL, mcpURL, "user-token")
	if err != nil {
		t.Fatalf("buildCurrentProbeRegistry() error = %v", err)
	}
	probe := requireProbeIdentity(t, registry, "api:GET /api/v0/images/tag-history")
	if !probe.execute {
		t.Fatal("tag-history probe execute = false, want an executable read fixture")
	}

	selectors := map[string]string{
		"repository_id":     "repository:r_fixture",
		"oci_repository_id": "oci-registry://ghcr.io/eshu-hq/demo",
		"tag":               "latest",
	}
	resolved, err := resolveProbeSelectors(probe, selectors)
	if err != nil {
		t.Fatalf("resolveProbeSelectors() error = %v", err)
	}
	repositoryID, _ := resolved.query["repository_id"].(string)
	if !strings.HasPrefix(repositoryID, "oci-registry://") {
		t.Fatalf("resolved repository_id = %q, want the oci-registry:// class, not the git-shaped repository_id", repositoryID)
	}
	if tag, _ := resolved.query["tag"].(string); tag != "latest" {
		t.Fatalf("resolved tag = %q, want latest", tag)
	}
	values := url.Values{}
	for name, value := range resolved.query {
		values.Set(name, fmt.Sprint(value))
	}
	if strings.Contains(values.Encode(), "repository%3Ar_fixture") {
		t.Fatalf("fixture query = %q, want no git-shaped repository_id", values.Encode())
	}
}
