package mcp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestDispatchToolDocumentationListsAllowScopedRoutes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		toolName   string
		args       map[string]any
		path       string
		capability string
	}{
		{
			name:     "findings",
			toolName: "list_documentation_findings",
			args: map[string]any{
				"repo":  "repository:team-a",
				"limit": 10,
			},
			path:       "/api/v0/documentation/findings",
			capability: "documentation_findings.list",
		},
		{
			name:     "facts",
			toolName: "list_documentation_facts",
			args: map[string]any{
				"repo":      "repository:team-a",
				"fact_kind": "section",
				"limit":     10,
			},
			path:       "/api/v0/documentation/facts",
			capability: "documentation_facts.list",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mux := http.NewServeMux()
			mux.HandleFunc("GET "+tc.path, func(w http.ResponseWriter, r *http.Request) {
				if _, ok := query.AuthContextFromContext(r.Context()); !ok {
					t.Fatal("AuthContextFromContext() ok = false, want true")
				}
				query.WriteSuccess(w, r, http.StatusOK, map[string]any{
					"count":     0,
					"limit":     10,
					"truncated": false,
				}, query.BuildTruthEnvelope(
					query.ProfileProduction,
					tc.capability,
					query.TruthBasisSemanticFacts,
					"test documentation route",
				))
			})
			resolver := &mcpScopedTokenResolver{
				auth: query.AuthContext{
					Mode:                 query.AuthModeScoped,
					TenantID:             "tenant-a",
					WorkspaceID:          "workspace-a",
					AllowedRepositoryIDs: []string{"repository:team-a"},
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
