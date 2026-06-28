// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestCodeFlowToolsAreRegisteredWithBoundedSchemas(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"dispatch_taint_path",
		"dispatch_reaching_def",
		"dispatch_cfg_summary",
		"dispatch_pdg_summary",
	} {
		tool := requireToolDefinition(t, name)
		schema := tool.InputSchema.(map[string]any)
		required := schema["required"].([]string)
		if len(required) != 1 || required[0] != "repo_id" {
			t.Fatalf("%s required = %#v, want repo_id only", name, required)
		}
		properties := schema["properties"].(map[string]any)
		if _, ok := properties["language"]; !ok {
			t.Fatalf("%s schema missing language filter", name)
		}
		limit := properties["limit"].(map[string]any)
		if got, want := limit["maximum"], 100; got != want {
			t.Fatalf("%s limit maximum = %#v, want %#v", name, got, want)
		}
		if got, want := limit["minimum"], 1; got != want {
			t.Fatalf("%s limit minimum = %#v, want %#v", name, got, want)
		}
	}
}

func TestResolveRouteMapsCodeFlowToolsToBoundedEndpoints(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"dispatch_taint_path":   "/api/v0/code/flow/taint-path",
		"dispatch_reaching_def": "/api/v0/code/flow/reaching-def",
		"dispatch_cfg_summary":  "/api/v0/code/flow/cfg-summary",
		"dispatch_pdg_summary":  "/api/v0/code/flow/pdg-summary",
	}
	for toolName, wantPath := range cases {
		route, err := resolveRoute(toolName, map[string]any{
			"repo_id":   "repo-1",
			"language":  "go",
			"symbol":    "handle",
			"file_path": "src/handler.go",
			"line":      float64(12),
			"limit":     float64(5),
		})
		if err != nil {
			t.Fatalf("resolveRoute(%s) error = %v, want nil", toolName, err)
		}
		if got, want := route.method, http.MethodPost; got != want {
			t.Fatalf("%s method = %q, want %q", toolName, got, want)
		}
		if route.path != wantPath {
			t.Fatalf("%s path = %q, want %q", toolName, route.path, wantPath)
		}
		body := route.body.(map[string]any)
		if got, want := body["repo_id"], "repo-1"; got != want {
			t.Fatalf("%s repo_id = %#v, want %#v", toolName, got, want)
		}
		if got, want := body["limit"], 5; got != want {
			t.Fatalf("%s limit = %#v, want %#v", toolName, got, want)
		}
	}
}

func TestDispatchToolCodeFlowAllowsScopedRoutes(t *testing.T) {
	t.Parallel()

	resolver := &mcpScopedTokenResolver{
		auth: query.AuthContext{
			Mode:                 query.AuthModeScoped,
			TenantID:             "tenant-a",
			WorkspaceID:          "workspace-a",
			AllowedRepositoryIDs: []string{"repo-1"},
		},
		ok: true,
	}
	handler := query.AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, http.MethodPost; got != want {
			t.Fatalf("method = %q, want %q", got, want)
		}
		if got, want := r.URL.Path, "/api/v0/code/flow/cfg-summary"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		if _, ok := query.AuthContextFromContext(r.Context()); !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		query.WriteSuccess(w, r, http.StatusOK, map[string]any{
			"functions": []any{},
			"coverage":  map[string]any{"state": "partial"},
			"bounds":    map[string]any{"limit": 5, "truncated": false},
		}, query.BuildTruthEnvelope(
			query.ProfileLocalAuthoritative,
			"code_flow.cfg_summary",
			query.TruthBasisContentIndex,
			"resolved from bounded code-flow parser facts",
		))
	}))

	result, err := dispatchTool(
		context.Background(),
		handler,
		"dispatch_cfg_summary",
		map[string]any{"repo_id": "repo-1", "language": "go", "limit": 5},
		"Bearer scoped-token",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want code-flow envelope")
	}
	if result.IsError {
		t.Fatalf("dispatchTool() IsError = true, want false; envelope = %#v", result.Envelope)
	}
}
