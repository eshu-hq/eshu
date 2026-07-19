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

func TestDispatchToolPackageRegistryCorrelationsAllowsScopedRoute(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/package-registry/correlations", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := query.AuthContextFromContext(r.Context()); !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		query.WriteSuccess(w, r, http.StatusOK, map[string]any{
			"correlations": []any{},
			"count":        0,
			"limit":        10,
			"truncated":    false,
		}, query.BuildTruthEnvelope(
			query.ProfileProduction,
			"package_registry.correlations.list",
			query.TruthBasisSemanticFacts,
			"test package registry correlations route",
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
		"list_package_registry_correlations",
		map[string]any{"repository_id": "repo-team-a", "limit": 10},
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

// TestDispatchToolPackageRegistryIdentityRoutesAllowScopedRoute is the
// MCP-dispatch-level counterpart of the #5167 W5b HTTP-handler scoped-access
// proof (package_registry_scoped_access_test.go): it proves each of the 5
// newly-scoped package-registry identity/aggregate tools actually threads
// the caller's AuthContext through dispatchTool's HTTP round trip to the real
// mux route, the same way TestDispatchToolPackageRegistryCorrelationsAllowsScopedRoute
// already proves for list_package_registry_correlations. The HTTP-handler
// tests prove the visibility/correlation gating logic is correct GIVEN an
// AuthContext in the request context; this proves dispatchTool's route
// mapping and query construction for these 5 tools actually deliver one.
func TestDispatchToolPackageRegistryIdentityRoutesAllowScopedRoute(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		tool       string
		args       map[string]any
		path       string
		body       map[string]any
		capability string
	}{
		{
			name:       "packages",
			tool:       "list_package_registry_packages",
			args:       map[string]any{"ecosystem": "npm", "limit": 10},
			path:       "GET /api/v0/package-registry/packages",
			body:       map[string]any{"packages": []any{}, "identity_issues": []any{}, "count": 0, "limit": 10, "truncated": false},
			capability: "package_registry.packages.list",
		},
		{
			name:       "versions",
			tool:       "list_package_registry_versions",
			args:       map[string]any{"package_id": "pkg:npm:left-pad", "limit": 10},
			path:       "GET /api/v0/package-registry/versions",
			body:       map[string]any{"versions": []any{}, "count": 0, "limit": 10, "truncated": false},
			capability: "package_registry.versions.list",
		},
		{
			name:       "dependencies",
			tool:       "list_package_registry_dependencies",
			args:       map[string]any{"package_id": "pkg:npm:left-pad", "limit": 10},
			path:       "GET /api/v0/package-registry/dependencies",
			body:       map[string]any{"dependencies": []any{}, "count": 0, "limit": 10, "truncated": false},
			capability: "package_registry.dependencies.list",
		},
		{
			name:       "count",
			tool:       "count_package_registry_packages",
			args:       map[string]any{"ecosystem": "npm"},
			path:       "GET /api/v0/package-registry/packages/count",
			body:       map[string]any{"total_packages": 0, "by_ecosystem": map[string]any{}, "scope": map[string]any{}},
			capability: "package_registry.packages.aggregate",
		},
		{
			name: "inventory",
			tool: "get_package_registry_package_inventory",
			args: map[string]any{"group_by": "ecosystem", "limit": 10},
			path: "GET /api/v0/package-registry/packages/inventory",
			body: map[string]any{
				"buckets": []any{}, "count": 0, "limit": 10, "offset": 0,
				"group_by": "ecosystem", "truncated": false, "next_offset": nil, "scope": map[string]any{},
			},
			capability: "package_registry.packages.aggregate",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mux := http.NewServeMux()
			// This handler runs on the dispatch goroutine, so it MUST NOT call
			// t.Fatal: FailNow from a non-owning goroutine panics the package
			// test binary under Go 1.26 (#2152). A missing AuthContext is
			// surfaced as a 5xx error and asserted on the subtest goroutine via
			// result.IsError below.
			mux.HandleFunc(tc.path, func(w http.ResponseWriter, r *http.Request) {
				if _, ok := query.AuthContextFromContext(r.Context()); !ok {
					query.WriteError(w, http.StatusInternalServerError, "AuthContextFromContext() ok = false, want true")
					return
				}
				query.WriteSuccess(w, r, http.StatusOK, tc.body, query.BuildTruthEnvelope(
					query.ProfileProduction,
					tc.capability,
					query.TruthBasisAuthoritativeGraph,
					"test package registry identity route",
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
