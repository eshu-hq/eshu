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

// TestDispatchToolWorkItemEvidenceAllowsScopedRoute proves the source-only
// work-item evidence tool dispatches through the scoped route gate with the
// AuthContext attached, so a scoped hosted token can read bounded work-item
// evidence intersected against its granted repository links (#2142). HTTP and
// MCP share the same handler and grant predicate, so authorizing the route here
// keeps API and MCP at parity.
func TestDispatchToolWorkItemEvidenceAllowsScopedRoute(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/work-items/evidence", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := query.AuthContextFromContext(r.Context()); !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		query.WriteSuccess(w, r, http.StatusOK, map[string]any{
			"evidence":         []any{},
			"count":            0,
			"limit":            10,
			"truncated":        false,
			"missing_evidence": true,
			"states":           []string{"missing_evidence"},
		}, query.BuildTruthEnvelope(
			query.ProfileProduction,
			"work_item.evidence.list",
			query.TruthBasisSemanticFacts,
			"test work-item evidence route",
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
		"list_work_item_evidence",
		map[string]any{"work_item_key": "OPS-123", "limit": 10},
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

// TestDispatchToolWorkItemEvidenceEmptyGrantStaysBoundedEmpty proves an
// empty-grant scoped token reaches the handler (the route is authorized) and the
// handler returns the bounded zero-evidence page without a store read, matching
// the API fail-closed posture.
func TestDispatchToolWorkItemEvidenceEmptyGrantStaysBoundedEmpty(t *testing.T) {
	t.Parallel()

	store := &boundedEmptyWorkItemEvidenceStore{}
	workItemHandler := &query.WorkItemHandler{Evidence: store, Profile: query.ProfileProduction}
	mux := http.NewServeMux()
	workItemHandler.Mount(mux)

	resolver := &mcpScopedTokenResolver{
		auth: query.AuthContext{
			Mode:        query.AuthModeScoped,
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-a",
		},
		ok: true,
	}
	handler := query.AuthMiddlewareWithScopedTokens("", resolver, mux)

	result, err := dispatchTool(
		context.Background(),
		handler,
		"list_work_item_evidence",
		map[string]any{"work_item_key": "OPS-123", "limit": 10},
		"Bearer scoped-token",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.IsError {
		t.Fatalf("dispatchTool() IsError = true, want false; envelope = %#v", result.Envelope)
	}
	if store.called {
		t.Fatal("work-item evidence store was called for empty scoped grants")
	}
}

type boundedEmptyWorkItemEvidenceStore struct {
	called bool
}

func (s *boundedEmptyWorkItemEvidenceStore) ListWorkItemEvidence(
	context.Context,
	query.WorkItemEvidenceFilter,
) (query.WorkItemEvidencePage, error) {
	s.called = true
	return query.WorkItemEvidencePage{}, nil
}
