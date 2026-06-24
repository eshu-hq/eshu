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

func TestDispatchToolEvidenceCitationAllowsScopedCitationRoute(t *testing.T) {
	t.Parallel()

	resolver := &mcpScopedTokenResolver{
		auth: query.AuthContext{
			Mode:                 query.AuthModeScoped,
			TenantID:             "tenant-a",
			WorkspaceID:          "workspace-a",
			AllowedRepositoryIDs: []string{"repo-team-a"},
		},
		ok: true,
	}
	handler := query.AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, http.MethodPost; got != want {
			t.Fatalf("method = %q, want %q", got, want)
		}
		if got, want := r.URL.Path, "/api/v0/evidence/citations"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		if _, ok := query.AuthContextFromContext(r.Context()); !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		query.WriteSuccess(w, r, http.StatusOK, map[string]any{
			"citations":       []any{},
			"missing_handles": []any{},
			"coverage": map[string]any{
				"resolved_count": 0,
				"missing_count":  0,
				"limit":          10,
				"truncated":      false,
			},
		}, query.BuildTruthEnvelope(
			query.ProfileLocalAuthoritative,
			"evidence_citation.packet",
			query.TruthBasisContentIndex,
			"resolved from bounded Postgres content handles without graph traversal",
		))
	}))

	result, err := dispatchTool(
		context.Background(),
		handler,
		"build_evidence_citation_packet",
		map[string]any{
			"handles": []any{map[string]any{
				"kind":          "file",
				"repo_id":       "repo-team-a",
				"relative_path": "README.md",
			}},
			"limit": 10,
		},
		"Bearer scoped-token",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want citation envelope")
	}
	if result.IsError {
		t.Fatalf("dispatchTool() IsError = true, want false; envelope = %#v", result.Envelope)
	}
	if result.Envelope.Truth == nil || result.Envelope.Truth.Capability != "evidence_citation.packet" {
		t.Fatalf("truth = %#v, want citation truth", result.Envelope.Truth)
	}
}
