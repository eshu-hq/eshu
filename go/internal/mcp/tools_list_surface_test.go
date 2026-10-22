// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/router"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func toolNames(tools []ToolDefinition) []string {
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		out = append(out, tool.Name)
	}
	return out
}

// wantSurface builds the expected tools/list names for surface over registry.
func wantSurface(registry []ToolDefinition, surface router.Surface) []string {
	switch surface {
	case router.SurfaceRouter:
		return []string{router.FindToolName, router.CallToolName}
	case router.SurfaceCoreRouter:
		core := map[string]bool{}
		for _, n := range router.CoreToolNames() {
			core[n] = true
		}
		var out []string
		for _, tool := range registry {
			if core[tool.Name] {
				out = append(out, tool.Name)
			}
		}
		return append(out, router.FindToolName, router.CallToolName)
	default:
		return toolNames(registry)
	}
}

// TestToolsListSurfaceModes covers every surface, with and without the
// production query profile, in both option orders. Hidden tools stay hidden
// in every mode because the surface applies to the profile-filtered list.
func TestToolsListSurfaceModes(t *testing.T) {
	production := filterToolsForProfile(ReadOnlyTools(), querycontract.ProfileProduction)
	tests := []struct {
		name     string
		surface  router.Surface
		profile  querycontract.QueryProfile
		surfaceB bool // apply WithToolSurface before WithQueryProfile
		registry []ToolDefinition
	}{
		{name: "full local", surface: router.SurfaceFull, registry: ReadOnlyTools()},
		{name: "router local", surface: router.SurfaceRouter, registry: ReadOnlyTools()},
		{name: "core_router local", surface: router.SurfaceCoreRouter, registry: ReadOnlyTools()},
		{name: "full production", surface: router.SurfaceFull, profile: querycontract.ProfileProduction, registry: production},
		{name: "router production", surface: router.SurfaceRouter, profile: querycontract.ProfileProduction, registry: production},
		{name: "core_router production", surface: router.SurfaceCoreRouter, profile: querycontract.ProfileProduction, registry: production},
		{name: "full production surface first", surface: router.SurfaceFull, profile: querycontract.ProfileProduction, surfaceB: true, registry: production},
		{name: "router production surface first", surface: router.SurfaceRouter, profile: querycontract.ProfileProduction, surfaceB: true, registry: production},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := []ServerOption{WithQueryProfile(tt.profile), WithToolSurface(tt.surface)}
			if tt.surfaceB {
				opts = []ServerOption{WithToolSurface(tt.surface), WithQueryProfile(tt.profile)}
			}
			s := NewServer(testServer().handler, nil, opts...)
			_, got := toolsListResult(t, s)
			if want := wantSurface(tt.registry, tt.surface); !reflect.DeepEqual(toolNames(got), want) {
				t.Fatalf("tools/list = %d tools %v, want %d tools %v", len(got), toolNames(got), len(want), want)
			}
		})
	}
}

// TestToolsListFullUnderProductionEqualsFilteredRegistry carries the #8762
// review item: full under the production profile is byte-identical to the
// profile-filtered registry.
func TestToolsListFullUnderProductionEqualsFilteredRegistry(t *testing.T) {
	s := NewServer(testServer().handler, nil,
		WithQueryProfile(querycontract.ProfileProduction), WithToolSurface(router.SurfaceFull))
	raw, got := toolsListResult(t, s)
	want := filterToolsForProfile(ReadOnlyTools(), querycontract.ProfileProduction)
	if msg := toolsListMismatch(raw, got, want); msg != "" {
		t.Fatal(msg)
	}
}

// TestToolsListFullWithSurfaceOptionIsByteIdenticalToDefault proves the
// explicit full surface changes no byte of tools/list.
func TestToolsListFullWithSurfaceOptionIsByteIdenticalToDefault(t *testing.T) {
	defaultRaw, _ := toolsListResult(t, testServer())
	fullRaw, _ := toolsListResult(t, NewServer(testServer().handler, nil, WithToolSurface(router.SurfaceFull)))
	if string(defaultRaw) != string(fullRaw) {
		t.Fatal("explicit full surface changed tools/list bytes")
	}
}

// TestDirectToolCallStillDispatchesWhenUnlisted proves the surface changes
// advertisement only: an unlisted registered tool still dispatches.
func TestDirectToolCallStillDispatchesWhenUnlisted(t *testing.T) {
	s := NewServer(testServer().handler, nil, WithToolSurface(router.SurfaceRouter))
	_, listed := toolsListResult(t, s)
	for _, tool := range listed {
		if tool.Name == "list_indexed_repositories" {
			t.Fatal("list_indexed_repositories must be unlisted under router")
		}
	}
	params, _ := json.Marshal(mcpToolCallParams{Name: "list_indexed_repositories", Arguments: map[string]any{}})
	resp := s.handleMessage(context.Background(), &jsonrpcRequest{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params}, "")
	if resp == nil || resp.Error != nil {
		t.Fatalf("tools/call response = %+v, want result", resp)
	}
	result, ok := resp.Result.(mcpToolResult)
	if !ok {
		t.Fatalf("result is %T, want mcpToolResult", resp.Result)
	}
	if result.IsError {
		t.Fatalf("unlisted tool call returned error result: %+v", result)
	}
}

func postToolsList(t *testing.T, s *Server, query string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp/message"+query,
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	rec := httptest.NewRecorder()
	s.handleHTTPMessage(rec, req)
	body, _ := io.ReadAll(rec.Body)
	return rec.Code, string(body)
}

func listedNames(t *testing.T, body string) []string {
	t.Helper()
	var decoded struct {
		Result struct {
			Tools []ToolDefinition `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return toolNames(decoded.Result.Tools)
}

// TestSurfaceQueryParamOverridesPerConnection proves the surface query
// parameter overrides the process setting for that request only.
func TestSurfaceQueryParamOverridesPerConnection(t *testing.T) {
	s := NewServer(testServer().handler, nil, WithToolSurface(router.SurfaceFull))
	registry := ReadOnlyTools()

	code, body := postToolsList(t, s, "?surface=router")
	if code != http.StatusOK {
		t.Fatalf("surface=router status = %d, body %s", code, body)
	}
	if got, want := listedNames(t, body), wantSurface(registry, router.SurfaceRouter); !reflect.DeepEqual(got, want) {
		t.Fatalf("override = %v, want %v", got, want)
	}

	code, body = postToolsList(t, s, "")
	if code != http.StatusOK {
		t.Fatalf("no param status = %d", code)
	}
	if got, want := listedNames(t, body), toolNames(registry); !reflect.DeepEqual(got, want) {
		t.Fatalf("override leaked into the next connection: got %d tools, want %d", len(got), len(want))
	}

	// An override beats a non-default process setting too.
	router1 := NewServer(testServer().handler, nil, WithToolSurface(router.SurfaceRouter))
	_, body = postToolsList(t, router1, "?surface=full")
	if got, want := listedNames(t, body), toolNames(registry); !reflect.DeepEqual(got, want) {
		t.Fatalf("surface=full over router = %d tools, want %d", len(got), len(want))
	}
}

// TestSurfaceQueryParamRejectsUnknownValue proves an unknown surface gets a
// 400 that names the allowed values.
func TestSurfaceQueryParamRejectsUnknownValue(t *testing.T) {
	s := testServer()
	code, body := postToolsList(t, s, "?surface=everything")
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %s", code, body)
	}
	for _, want := range []string{"full", "router", "core_router"} {
		if !strings.Contains(body, want) {
			t.Fatalf("400 body %q does not list %q", body, want)
		}
	}
}

// TestSurfaceOverrideKeepsProductionHiddenToolsHidden proves an override
// composes with the production profile.
func TestSurfaceOverrideKeepsProductionHiddenToolsHidden(t *testing.T) {
	s := NewServer(testServer().handler, nil, WithQueryProfile(querycontract.ProfileProduction))
	_, body := postToolsList(t, s, "?surface=full")
	want := toolNames(filterToolsForProfile(ReadOnlyTools(), querycontract.ProfileProduction))
	if got := listedNames(t, body); !reflect.DeepEqual(got, want) {
		t.Fatalf("override under production = %d tools, want %d", len(got), len(want))
	}
}

// TestToolSurfaceSummary reports the surface, advertised count, and byte size
// the startup log carries.
func TestToolSurfaceSummary(t *testing.T) {
	s := NewServer(testServer().handler, nil, WithToolSurface(router.SurfaceRouter))
	surface, count, size := s.ToolSurfaceSummary()
	raw, _ := toolsListResult(t, s)
	if surface != router.SurfaceRouter || count != 2 || size != len(raw) {
		t.Fatalf("summary = (%q, %d, %d), want (router, 2, %d)", surface, count, size, len(raw))
	}
}
