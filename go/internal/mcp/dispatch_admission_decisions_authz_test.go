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

func TestDispatchToolAdmissionDecisionsAllowsScopedRoute(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/evidence/admission-decisions", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := query.AuthContextFromContext(r.Context()); !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		query.WriteSuccess(w, r, http.StatusOK, map[string]any{
			"decisions":              []any{},
			"count":                  0,
			"limit":                  10,
			"truncated":              false,
			"recommended_next_calls": []any{},
		}, query.BuildTruthEnvelope(
			query.ProfileProduction,
			"admission_decisions.list",
			query.TruthBasisSemanticFacts,
			"test admission decisions route",
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
		"list_admission_decisions",
		map[string]any{
			"domain":        "deployable_unit",
			"scope_id":      "repo-team-a",
			"generation_id": "generation-1",
			"limit":         10,
		},
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
