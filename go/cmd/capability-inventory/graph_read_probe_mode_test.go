// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestRunGraphReadProbeUsesDeclaredArgumentsAndAuth(t *testing.T) {
	const userToken = "user-secret"
	const adminToken = "admin-secret"
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		wantToken := userToken
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
		if request.Header.Get("Authorization") != "Bearer "+wantToken {
			t.Errorf("API auth = %q, want bearer token for declared posture", request.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
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
	if err == nil || !strings.Contains(err.Error(), "current surfaces lack checked-in fixtures") {
		t.Fatalf("runGraphReadProbe() error = %v, want explicit current-surface fixture gap", err)
	}
	if strings.Count(output.String(), "PASS") != len(graphReadProbeRegistry) {
		t.Fatalf("output = %q, want one PASS per registered surface", output.String())
	}
}

func TestRunGraphReadProbeFailsClosedOnUnsupportedSurface(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	err := runGraphReadProbe(&strings.Builder{}, server.Client(), server.URL, server.URL, "user", "admin")
	if err == nil || !strings.Contains(err.Error(), "unsupported or failed") {
		t.Fatalf("runGraphReadProbe() error = %v, want explicit unsupported-surface failure", err)
	}
}
