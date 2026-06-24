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

func TestDispatchToolQueryPlaybooksAllowsScopedRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		toolName string
		args     map[string]any
		method   string
		path     string
	}{
		{
			name:     "list",
			toolName: "list_query_playbooks",
			args:     map[string]any{},
			method:   http.MethodGet,
			path:     "/api/v0/query-playbooks",
		},
		{
			name:     "resolve",
			toolName: "resolve_query_playbook",
			args: map[string]any{
				"playbook_id": "service_story_citation",
				"inputs": map[string]any{
					"service_name": "payments",
				},
			},
			method: http.MethodPost,
			path:   "/api/v0/query-playbooks/resolve",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
				if got := r.Method; got != tt.method {
					t.Fatalf("method = %q, want %q", got, tt.method)
				}
				if got := r.URL.Path; got != tt.path {
					t.Fatalf("path = %q, want %q", got, tt.path)
				}
				if _, ok := query.AuthContextFromContext(r.Context()); !ok {
					t.Fatal("AuthContextFromContext() ok = false, want true")
				}
				query.WriteSuccess(w, r, http.StatusOK, map[string]any{
					"schema_version": "query-playbooks.v1",
				}, query.BuildTruthEnvelope(
					query.ProfileLocalAuthoritative,
					query.CapabilityQueryPlaybooks,
					query.TruthBasisRuntimeState,
					"deterministic query playbook catalog; no live backend read",
				))
			}))

			result, err := dispatchTool(
				context.Background(),
				handler,
				tt.toolName,
				tt.args,
				"Bearer scoped-token",
				slog.New(slog.NewTextHandler(io.Discard, nil)),
			)
			if err != nil {
				t.Fatalf("dispatchTool() error = %v, want nil", err)
			}
			if result.Envelope == nil {
				t.Fatal("dispatchTool() envelope is nil, want query playbook envelope")
			}
			if result.IsError {
				t.Fatalf("dispatchTool() IsError = true, want false; envelope = %#v", result.Envelope)
			}
			if result.Envelope.Truth == nil || result.Envelope.Truth.Capability != query.CapabilityQueryPlaybooks {
				t.Fatalf("truth = %#v, want query playbooks truth", result.Envelope.Truth)
			}
		})
	}
}
