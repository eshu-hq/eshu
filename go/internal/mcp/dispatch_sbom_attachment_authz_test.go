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

// TestDispatchToolSBOMAttachmentAllowsScopedRoutes proves the three
// reducer-owned SBOM/attestation attachment read tools dispatch through the
// scoped route gate with the AuthContext attached (#2133).
func TestDispatchToolSBOMAttachmentAllowsScopedRoutes(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	okEnvelope := func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if _, ok := query.AuthContextFromContext(r.Context()); !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		query.WriteSuccess(w, r, http.StatusOK, body, query.BuildTruthEnvelope(
			query.ProfileProduction,
			"supply_chain.sbom_attestation_attachments.list",
			query.TruthBasisSemanticFacts,
			"test sbom attachment route",
		))
	}
	mux.HandleFunc("GET /api/v0/supply-chain/sbom-attestations/attachments", func(w http.ResponseWriter, r *http.Request) {
		okEnvelope(w, r, map[string]any{"attachments": []any{}, "count": 0, "limit": 10, "truncated": false})
	})
	mux.HandleFunc("GET /api/v0/supply-chain/sbom-attestations/attachments/count", func(w http.ResponseWriter, r *http.Request) {
		okEnvelope(w, r, map[string]any{"total_attachments": 0})
	})
	mux.HandleFunc("GET /api/v0/supply-chain/sbom-attestations/attachments/inventory", func(w http.ResponseWriter, r *http.Request) {
		okEnvelope(w, r, map[string]any{"buckets": []any{}, "count": 0, "limit": 10, "group_by": "artifact_kind", "truncated": false})
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
		{name: "list", tool: "list_sbom_attestation_attachments", args: map[string]any{"repository_id": "repo-team-a", "limit": 10}},
		{name: "count", tool: "count_sbom_attestation_attachments", args: map[string]any{"repository_id": "repo-team-a"}},
		{name: "inventory", tool: "get_sbom_attestation_attachment_inventory", args: map[string]any{"repository_id": "repo-team-a", "group_by": "artifact_kind", "limit": 10}},
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
