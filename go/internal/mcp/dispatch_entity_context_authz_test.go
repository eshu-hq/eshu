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

func TestDispatchToolEntityContextAllowsScopedEntityContextRoute(t *testing.T) {
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
	handler := query.AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, http.MethodGet; got != want {
			t.Fatalf("method = %q, want %q", got, want)
		}
		if got, want := r.URL.Path, "/api/v0/entities/entity-a/context"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		if _, ok := query.AuthContextFromContext(r.Context()); !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		query.WriteSuccess(w, r, http.StatusOK, map[string]any{
			"id":               "entity-a",
			"relationships":    []any{},
			"partial_reasons":  []any{},
			"result_limits":    map[string]any{"limit": 1, "truncated": false},
			"evidence_handles": []any{},
		}, query.BuildTruthEnvelope(
			query.ProfileLocalAuthoritative,
			"code_search.fuzzy_symbol",
			query.TruthBasisHybrid,
			"resolved from bounded entity context graph read",
		))
	}))

	result, err := dispatchTool(
		context.Background(),
		handler,
		"get_entity_context",
		map[string]any{"entity_id": "entity-a"},
		"Bearer scoped-token",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want entity context envelope")
	}
	if result.IsError {
		t.Fatalf("dispatchTool() IsError = true, want false; envelope = %#v", result.Envelope)
	}
	if result.Envelope.Truth == nil || result.Envelope.Truth.Capability != "code_search.fuzzy_symbol" {
		t.Fatalf("truth = %#v, want entity context truth", result.Envelope.Truth)
	}
}
