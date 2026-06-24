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

func TestDispatchToolCICDRunCorrelationsAllowsScopedRoutes(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/ci-cd/run-correlations", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := query.AuthContextFromContext(r.Context()); !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		query.WriteSuccess(w, r, http.StatusOK, map[string]any{
			"correlations": []any{},
			"count":        0,
			"limit":        10,
			"truncated":    false,
		}, query.BuildTruthEnvelope(
			query.ProfileProduction,
			"ci_cd.run_correlations.list",
			query.TruthBasisSemanticFacts,
			"test ci/cd run correlations route",
		))
	})
	mux.HandleFunc("GET /api/v0/ci-cd/run-correlations/count", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := query.AuthContextFromContext(r.Context()); !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		query.WriteSuccess(w, r, http.StatusOK, map[string]any{
			"total_correlations": 0,
			"by_outcome":         map[string]int{},
			"by_environment":     map[string]int{},
			"by_provider":        map[string]int{},
		}, query.BuildTruthEnvelope(
			query.ProfileProduction,
			"ci_cd.run_correlations.aggregate",
			query.TruthBasisSemanticFacts,
			"test ci/cd run correlation count route",
		))
	})
	mux.HandleFunc("GET /api/v0/ci-cd/run-correlations/inventory", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := query.AuthContextFromContext(r.Context()); !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		query.WriteSuccess(w, r, http.StatusOK, map[string]any{
			"buckets":   []any{},
			"count":     0,
			"limit":     10,
			"group_by":  "outcome",
			"truncated": false,
		}, query.BuildTruthEnvelope(
			query.ProfileProduction,
			"ci_cd.run_correlations.aggregate",
			query.TruthBasisSemanticFacts,
			"test ci/cd run correlation inventory route",
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
			name: "list",
			tool: "list_ci_cd_run_correlations",
			args: map[string]any{"repository_id": "repo-team-a", "limit": 10},
		},
		{
			name: "count",
			tool: "count_ci_cd_run_correlations",
			args: map[string]any{"repository_id": "repo-team-a"},
		},
		{
			name: "inventory",
			tool: "get_ci_cd_run_correlation_inventory",
			args: map[string]any{"repository_id": "repo-team-a", "limit": 10},
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
