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

// TestDispatchToolInfraSearchAndRelationshipsAllowScopedRoutes proves the infra
// resource search and relationship tools dispatch through the scoped route gate
// with the AuthContext attached, so a scoped hosted token reaches the
// tenant-filtered handler (#2158, #2159).
func TestDispatchToolInfraSearchAndRelationshipsAllowScopedRoutes(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v0/infra/resources/search", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := query.AuthContextFromContext(r.Context()); !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		query.WriteSuccess(w, r, http.StatusOK, map[string]any{
			"results": []any{}, "count": 0, "limit": 50, "truncated": false,
		}, query.BuildTruthEnvelope(
			query.ProfileProduction,
			"platform_impact.deployment_chain",
			query.TruthBasisHybrid,
			"test infra search route",
		))
	})
	mux.HandleFunc("POST /api/v0/infra/relationships", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := query.AuthContextFromContext(r.Context()); !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		query.WriteSuccess(w, r, http.StatusOK, map[string]any{
			"id": "tf:aws_s3_bucket.api", "name": "api", "labels": []any{"TerraformResource"},
			"outgoing": []any{}, "incoming": []any{},
		}, query.BuildTruthEnvelope(
			query.ProfileProduction,
			"platform_impact.deployment_chain",
			query.TruthBasisHybrid,
			"test infra relationships route",
		))
	})
	resolver := &mcpScopedTokenResolver{
		auth: query.AuthContext{
			Mode:                 query.AuthModeScoped,
			TenantID:             "tenant-a",
			WorkspaceID:          "workspace-a",
			AllowedRepositoryIDs: []string{"repo-team-a"},
		},
		ok: true,
	}
	handler := query.AuthMiddlewareWithScopedTokens("", resolver, mux)

	for _, tc := range []struct {
		name string
		tool string
		args map[string]any
	}{
		{
			name: "search",
			tool: "find_infra_resources",
			args: map[string]any{"query": "api", "limit": 50},
		},
		{
			name: "relationships",
			tool: "analyze_infra_relationships",
			args: map[string]any{"target": "tf:aws_s3_bucket.api"},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := dispatchTool(
				context.Background(),
				handler,
				tc.tool,
				tc.args,
				"Bearer scoped-token",
				slog.New(slog.NewTextHandler(io.Discard, nil)),
			)
			if err != nil {
				t.Fatalf("dispatchTool() error = %v, want nil", err)
			}
			if result.IsError {
				t.Fatalf("dispatchTool() IsError = true, want false; envelope = %#v", result.Envelope)
			}
			if result.Envelope == nil || result.Envelope.Truth == nil {
				t.Fatalf("envelope = %#v, want truth envelope", result.Envelope)
			}
		})
	}
}
