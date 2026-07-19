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

func TestDispatchToolFindCodeAllowsScopedCodeSearchRoute(t *testing.T) {
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
		if got, want := r.URL.Path, "/api/v0/code/search"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		if _, ok := query.AuthContextFromContext(r.Context()); !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode find_code body: %v", err)
		}
		if body["query"] != "Handle" || body["language"] != "go" || body["exact"] != true {
			t.Fatalf("find_code body = %#v, want global exact Go name lookup", body)
		}
		query.WriteSuccess(w, r, http.StatusOK, map[string]any{
			"source":  "content",
			"results": []any{},
			"matches": []any{},
		}, query.BuildTruthEnvelope(
			query.ProfileLocalAuthoritative,
			"code_search.fuzzy_symbol",
			query.TruthBasisContentIndex,
			"resolved from content index fallback",
		))
	}))

	result, err := dispatchTool(
		context.Background(),
		handler,
		"find_code",
		map[string]any{"query": "Handle", "language": "go", "exact": true, "limit": 5},
		"Bearer scoped-token",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want code search envelope")
	}
	if result.IsError {
		t.Fatalf("dispatchTool() IsError = true, want false; envelope = %#v", result.Envelope)
	}
	if result.Envelope.Truth == nil || result.Envelope.Truth.Capability != "code_search.fuzzy_symbol" {
		t.Fatalf("truth = %#v, want code search truth", result.Envelope.Truth)
	}
}

type mcpScopedTokenResolver struct {
	auth query.AuthContext
	ok   bool
}

func (r *mcpScopedTokenResolver) ResolveScopedToken(context.Context, string) (query.AuthContext, bool, error) {
	return r.auth, r.ok, nil
}
