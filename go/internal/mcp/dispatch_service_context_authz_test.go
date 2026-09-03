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

func TestDispatchToolServiceAndWorkloadContextAllowsScopedRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		toolName string
		args     map[string]any
		method   string
		path     string
	}{
		{
			name:     "workload context",
			toolName: "get_workload_context",
			args:     map[string]any{"workload_id": "workload:payments"},
			method:   http.MethodGet,
			path:     "/api/v0/workloads/workload:payments/context",
		},
		{
			name:     "workload story",
			toolName: "get_workload_story",
			args:     map[string]any{"workload_id": "workload:payments"},
			method:   http.MethodGet,
			path:     "/api/v0/workloads/workload:payments/story",
		},
		{
			name:     "service context",
			toolName: "get_service_context",
			args:     map[string]any{"workload_id": "workload:payments"},
			method:   http.MethodGet,
			path:     "/api/v0/services/payments/context",
		},
		{
			name:     "service story",
			toolName: "get_service_story",
			args:     map[string]any{"workload_id": "workload:payments"},
			method:   http.MethodGet,
			path:     "/api/v0/services/payments/story",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resolver := &mcpScopedTokenResolver{
				auth: query.AuthContext{
					Mode:        query.AuthModeScoped,
					TenantID:    "tenant-a",
					WorkspaceID: "workspace-a",
					AllScopes:   true,
				},
				ok: true,
			}
			handler := query.AuthMiddlewareWithScopedTokensAndRoutePolicy("", resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Method; got != tt.method {
					t.Fatalf("method = %q, want %q", got, tt.method)
				}
				if got := r.URL.Path; got != tt.path {
					t.Fatalf("path = %q, want %q", got, tt.path)
				}
				if _, ok := query.AuthContextFromContext(r.Context()); !ok {
					t.Fatal("AuthContextFromContext() ok = false, want true")
				}
				query.WriteSuccess(w, r, http.StatusOK, map[string]any{
					"id":              "workload:payments",
					"name":            "payments",
					"result_limits":   map[string]any{"limit": 1, "truncated": false},
					"partial_reasons": []any{},
				}, query.BuildTruthEnvelope(
					query.ProfileLocalAuthoritative,
					"platform_impact.context_overview",
					query.TruthBasisHybrid,
					"resolved from bounded service context graph read",
				))
			}),
				// The permissive route policy is what a local_no_policy or
				// hosted_single_tenant deployment wires (see
				// query.ScopedRoutePolicyForGovernanceMode). It is required here
				// because the resolver above returns an all-scope context, which
				// since #6450's residual item 1 closed is refused on a grant-bound
				// route under a fail-closed policy. This test is about MCP tool
				// dispatch reaching the right route, not about admission.
				query.BrowserSessionRoutePolicy{AllowTenantBoundAllScopes: true})

			result, err := dispatchTool(
				context.Background(),
				handler,
				tt.toolName,
				tt.args,
				"Bearer scoped-token",
				slog.New(slog.NewTextHandler(io.Discard, nil)),
			)
			if err != nil {
				t.Fatalf("dispatchTool() error = %v, want nil", err)
			}
			if result.IsError {
				t.Fatalf("dispatchTool() IsError = true, want false; envelope = %#v", result.Envelope)
			}
		})
	}
}
