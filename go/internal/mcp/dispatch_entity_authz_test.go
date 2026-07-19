// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestDispatchToolResolveEntityAllowsScopedEntityResolveRoute(t *testing.T) {
	t.Parallel()

	resolver := &mcpScopedTokenResolver{
		auth: query.AuthContext{
			Mode:        query.AuthModeScoped,
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-a",
		},
		ok: true,
	}
	handler := query.AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, http.MethodPost; got != want {
			t.Fatalf("method = %q, want %q", got, want)
		}
		if got, want := r.URL.Path, "/api/v0/entities/resolve"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		if _, ok := query.AuthContextFromContext(r.Context()); !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode resolve_entity body: %v", err)
		}
		if body["name"] != "HandlePayment" || body["type"] != "function" {
			t.Fatalf("resolve_entity body = %#v, want typed global exact lookup", body)
		}
		query.WriteSuccess(w, r, http.StatusOK, map[string]any{
			"entities":  []any{},
			"matches":   []any{},
			"count":     0,
			"limit":     5,
			"truncated": false,
		}, query.BuildTruthEnvelope(
			query.ProfileLocalAuthoritative,
			"code_search.exact_symbol",
			query.TruthBasisContentIndex,
			"resolved by exact case-sensitive name from the current content entity index",
		))
	}))

	result, err := dispatchTool(
		context.Background(),
		handler,
		"resolve_entity",
		map[string]any{"query": "HandlePayment", "type": "function", "limit": 5},
		"Bearer scoped-token",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want entity resolve envelope")
	}
	if result.IsError {
		t.Fatalf("dispatchTool() IsError = true, want false; envelope = %#v", result.Envelope)
	}
	if result.Envelope.Truth == nil || result.Envelope.Truth.Capability != "code_search.exact_symbol" ||
		result.Envelope.Truth.Basis != query.TruthBasisContentIndex {
		t.Fatalf("truth = %#v, want entity resolve truth", result.Envelope.Truth)
	}
}
