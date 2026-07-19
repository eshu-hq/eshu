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

// TestDispatchToolSupplyChainImpactAllowsScopedRoutes proves the three
// reducer-owned vulnerability impact read tools dispatch through the scoped
// route gate with the AuthContext attached, so a scoped hosted token can read
// bounded impact answers (#2124).
func TestDispatchToolSupplyChainImpactAllowsScopedRoutes(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	// okEnvelope runs on the dispatch goroutine (a parallel subtest's goroutine),
	// not the parent test goroutine, so it MUST NOT call t.Fatal: FailNow from a
	// non-owning goroutine panics the package test binary under Go 1.26. A missing
	// AuthContext is surfaced as a 5xx error response and asserted on the subtest
	// goroutine via result.IsError below (#2152).
	okEnvelope := func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if _, ok := query.AuthContextFromContext(r.Context()); !ok {
			query.WriteError(w, http.StatusInternalServerError, "AuthContextFromContext() ok = false, want true")
			return
		}
		query.WriteSuccess(w, r, http.StatusOK, body, query.BuildTruthEnvelope(
			query.ProfileProduction,
			"supply_chain.impact_findings.list",
			query.TruthBasisSemanticFacts,
			"test supply-chain impact route",
		))
	}
	mux.HandleFunc("GET /api/v0/supply-chain/impact/findings", func(w http.ResponseWriter, r *http.Request) {
		okEnvelope(w, r, map[string]any{
			"findings":  []any{},
			"count":     0,
			"limit":     10,
			"truncated": false,
		})
	})
	mux.HandleFunc("GET /api/v0/supply-chain/impact/findings/count", func(w http.ResponseWriter, r *http.Request) {
		okEnvelope(w, r, map[string]any{"total_findings": 0})
	})
	mux.HandleFunc("GET /api/v0/supply-chain/impact/inventory", func(w http.ResponseWriter, r *http.Request) {
		okEnvelope(w, r, map[string]any{
			"buckets":   []any{},
			"count":     0,
			"limit":     10,
			"group_by":  "ecosystem",
			"truncated": false,
		})
	})
	mux.HandleFunc("GET /api/v0/supply-chain/impact/explain", func(w http.ResponseWriter, r *http.Request) {
		okEnvelope(w, r, map[string]any{
			"outcome":   "no_finding",
			"input":     map[string]any{},
			"advisory":  map[string]any{},
			"component": map[string]any{},
			"version":   map[string]any{"version_evidence": "missing"},
			"anchors":   map[string]any{},
			"evidence":  []any{},
			"readiness": map[string]any{},
			"freshness": map[string]any{"state": "unknown", "evidence_fact_count": 0},
		})
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

	for _, tc := range []struct {
		name string
		tool string
		args map[string]any
	}{
		{
			name: "list",
			tool: "list_supply_chain_impact_findings",
			args: map[string]any{"repository_id": "repo-team-a", "limit": 10},
		},
		{
			name: "count",
			tool: "count_supply_chain_impact_findings",
			args: map[string]any{"repository_id": "repo-team-a"},
		},
		{
			name: "inventory",
			tool: "get_supply_chain_impact_inventory",
			args: map[string]any{"repository_id": "repo-team-a", "group_by": "ecosystem", "limit": 10},
		},
		{
			name: "explain",
			tool: "explain_supply_chain_impact",
			args: map[string]any{"repository_id": "repo-team-a", "advisory_id": "CVE-2026-0001"},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

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
