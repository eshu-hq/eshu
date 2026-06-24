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

// TestDispatchToolAdvisoryEvidenceAllowsScopedRoute proves the advisory-evidence
// read tool dispatches through the scoped route gate with the AuthContext
// attached. Advisory evidence is global CVE data; the gate enforces grants on
// the repository-anchored derivation, not on bare-id public lookups (#2056).
func TestDispatchToolAdvisoryEvidenceAllowsScopedRoute(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/supply-chain/advisories/evidence", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := query.AuthContextFromContext(r.Context()); !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		query.WriteSuccess(w, r, http.StatusOK, map[string]any{
			"advisories": []any{},
			"count":      0,
			"limit":      10,
		}, query.BuildTruthEnvelope(
			query.ProfileProduction,
			"supply_chain.advisory_evidence.list",
			query.TruthBasisSemanticFacts,
			"test advisory evidence route",
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
		"list_advisory_evidence",
		map[string]any{"cve_id": "CVE-2026-0001", "limit": 10},
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
}
