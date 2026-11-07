// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const probeStubOpenAPISpec = `{
  "openapi": "3.0.0",
  "paths": {
    "/api/v0/repositories": {"get": {"responses": {"200": {"description": "ok"}}}},
    "/api/v0/code/cypher": {"post": {"responses": {"200": {"description": "ok"}}}}
  }
}`

func TestFetchSatelliteOpenAPISpecReturnsDocument(t *testing.T) {
	t.Parallel()
	var gotAccept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v0/openapi.json" {
			t.Errorf("spec path = %q, want /api/v0/openapi.json", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "" {
			t.Errorf("spec request carried Authorization %q, want none (public doc route)", request.Header.Get("Authorization"))
		}
		gotAccept = request.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(probeStubOpenAPISpec))
	}))
	defer server.Close()

	document, err := fetchSatelliteOpenAPISpec(server.Client(), server.URL)
	if err != nil {
		t.Fatalf("fetchSatelliteOpenAPISpec() error = %v", err)
	}
	paths, _ := document["paths"].(map[string]any)
	if _, ok := paths["/api/v0/repositories"]; !ok {
		t.Fatalf("document paths = %#v, want /api/v0/repositories", paths)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}
}

func TestFetchSatelliteOpenAPISpecFailsClosed(t *testing.T) {
	t.Parallel()
	notFound := httptest.NewServer(http.NotFoundHandler())
	defer notFound.Close()
	if _, err := fetchSatelliteOpenAPISpec(notFound.Client(), notFound.URL); err == nil ||
		!strings.Contains(err.Error(), "satellite openapi HTTP status 404") {
		t.Fatalf("404 error = %v, want satellite openapi HTTP status 404", err)
	}

	badBody := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{invalid`))
	}))
	defer badBody.Close()
	if _, err := fetchSatelliteOpenAPISpec(badBody.Client(), badBody.URL); err == nil ||
		!strings.Contains(err.Error(), "decode satellite openapi") {
		t.Fatalf("malformed error = %v, want decode satellite openapi", err)
	}

	scalar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`["not","an","object"]`))
	}))
	defer scalar.Close()
	if _, err := fetchSatelliteOpenAPISpec(scalar.Client(), scalar.URL); err == nil ||
		!strings.Contains(err.Error(), "not a JSON object") {
		t.Fatalf("scalar error = %v, want not-a-object rejection", err)
	}

	if _, err := fetchSatelliteOpenAPISpec(notFound.Client(), "://bad-url"); err == nil ||
		!strings.Contains(err.Error(), "invalid API base URL") {
		t.Fatalf("bad URL error = %v, want invalid API base URL", err)
	}
	if _, err := fetchSatelliteOpenAPISpec(nil, notFound.URL); err == nil {
		t.Fatal("nil client error = nil, want failure")
	}
}

func TestFetchSatelliteOpenAPISpecSanitizesTransportErrors(t *testing.T) {
	t.Parallel()
	const privateCause = "dial https://private.example.invalid/api/v0/openapi.json Authorization=Bearer secret-marker"
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New(privateCause)
	})}
	_, err := fetchSatelliteOpenAPISpec(client, "https://private.example.invalid")
	if err == nil {
		t.Fatal("fetchSatelliteOpenAPISpec() error = nil, want sanitized transport failure")
	}
	for _, forbidden := range []string{privateCause, "private.example.invalid", "secret-marker", "Bearer"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("spec fetch error exposed %q: %v", forbidden, err)
		}
	}
}

func TestFetchSatelliteMCPToolsListsTools(t *testing.T) {
	t.Parallel()
	const userToken = "user-list-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("tools/list method = %s, want POST", request.Method)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer "+userToken {
			t.Errorf("tools/list auth = %q, want user Bearer [REDACTED]", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[
			{"name":"list_indexed_repositories","inputSchema":{"type":"object","properties":{"limit":{"type":"integer"}}}},
			{"name":"execute_cypher_query","inputSchema":{"type":"object","properties":{"cypher_query":{"type":"string"}}}}
		]}}`))
	}))
	defer server.Close()

	tools, err := fetchSatelliteMCPTools(server.Client(), server.URL, userToken)
	if err != nil {
		t.Fatalf("fetchSatelliteMCPTools() error = %v", err)
	}
	if len(tools) != 2 || tools[0].Name != "list_indexed_repositories" || tools[1].Name != "execute_cypher_query" {
		t.Fatalf("tools = %#v, want the two listed tools", tools)
	}
	schema, _ := tools[0].InputSchema.(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
	if _, ok := properties["limit"]; !ok {
		t.Fatalf("inputSchema = %#v, want limit property", tools[0].InputSchema)
	}
}

func TestFetchSatelliteMCPToolsFailsClosed(t *testing.T) {
	t.Parallel()
	rpcError := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method not found"}}`))
	}))
	defer rpcError.Close()
	if _, err := fetchSatelliteMCPTools(rpcError.Client(), rpcError.URL, "token"); err == nil ||
		!strings.Contains(err.Error(), "JSON-RPC error") {
		t.Fatalf("rpc error = %v, want JSON-RPC error", err)
	}

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer broken.Close()
	if _, err := fetchSatelliteMCPTools(broken.Client(), broken.URL, "token"); err == nil ||
		!strings.Contains(err.Error(), "MCP tools/list HTTP status 500") {
		t.Fatalf("500 error = %v, want MCP tools/list HTTP status 500", err)
	}

	if _, err := fetchSatelliteMCPTools(rpcError.Client(), "://bad-url", "token"); err == nil ||
		!strings.Contains(err.Error(), "invalid MCP URL") {
		t.Fatalf("bad URL error = %v, want invalid MCP URL", err)
	}
	if _, err := fetchSatelliteMCPTools(nil, rpcError.URL, "token"); err == nil {
		t.Fatal("nil client error = nil, want failure")
	}
}

func TestFetchSatelliteMCPToolsSanitizesTransportErrors(t *testing.T) {
	t.Parallel()
	const privateCause = "dial https://private.example.invalid/mcp Authorization=Bearer secret-marker"
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New(privateCause)
	})}
	_, err := fetchSatelliteMCPTools(client, "https://private.example.invalid/mcp", "secret-marker")
	if err == nil {
		t.Fatal("fetchSatelliteMCPTools() error = nil, want sanitized transport failure")
	}
	for _, forbidden := range []string{privateCause, "private.example.invalid", "secret-marker", "Bearer"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("tools/list error exposed %q: %v", forbidden, err)
		}
	}
}

func TestLocalOpenAPISpecDocumentParsesLiveSpec(t *testing.T) {
	t.Parallel()
	document, err := localOpenAPISpecDocument()
	if err != nil {
		t.Fatalf("localOpenAPISpecDocument() error = %v", err)
	}
	paths, _ := document["paths"].(map[string]any)
	if len(paths) == 0 {
		t.Fatal("local spec holds zero paths, want the live assembled set")
	}
	repositories, _ := paths["/api/v0/repositories"].(map[string]any)
	if _, ok := repositories["get"]; !ok {
		t.Fatalf("repositories path = %#v, want a get operation", repositories)
	}
}
