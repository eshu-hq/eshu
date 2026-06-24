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

func TestDispatchToolSemanticEvidenceAllowsScopedRoutes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		toolName   string
		args       map[string]any
		path       string
		capability string
	}{
		{
			name:     "documentation observations",
			toolName: "list_semantic_documentation_observations",
			args: map[string]any{
				"repo":                "repo-team-a",
				"provider_profile_id": "semantic-docs-default",
				"limit":               10,
			},
			path:       "/api/v0/semantic/documentation-observations",
			capability: "semantic_evidence.documentation_observations.list",
		},
		{
			name:     "code hints",
			toolName: "list_semantic_code_hints",
			args: map[string]any{
				"repo":                "repo-team-a",
				"provider_profile_id": "semantic-code-default",
				"limit":               10,
			},
			path:       "/api/v0/semantic/code-hints",
			capability: "semantic_evidence.code_hints.list",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mux := http.NewServeMux()
			mux.HandleFunc("GET "+tc.path, func(w http.ResponseWriter, r *http.Request) {
				query.WriteSuccess(w, r, http.StatusOK, map[string]any{
					"count":     0,
					"limit":     10,
					"truncated": false,
				}, query.BuildTruthEnvelope(
					query.ProfileProduction,
					tc.capability,
					query.TruthBasisSemanticFacts,
					"test semantic evidence route",
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

			result, err := dispatchTool(
				context.Background(),
				handler,
				tc.toolName,
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
