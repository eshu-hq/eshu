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

func TestDispatchToolInvestigateServiceAllowsScopedRoute(t *testing.T) {
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
		if got, want := r.URL.Path, "/api/v0/investigations/services/payments"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		if _, ok := query.AuthContextFromContext(r.Context()); !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		query.WriteSuccess(w, r, http.StatusOK, map[string]any{
			"service_name":               "payments",
			"repositories_considered":    []any{},
			"repositories_with_evidence": []any{},
			"evidence_families_found":    []any{},
			"coverage_summary":           map[string]any{"state": "unknown"},
			"investigation_findings":     []any{},
			"recommended_next_calls":     []any{},
		}, query.BuildTruthEnvelope(
			query.ProfileLocalAuthoritative,
			"platform_impact.context_overview",
			query.TruthBasisHybrid,
			"resolved from bounded service investigation graph read",
		))
	}))

	result, err := dispatchTool(
		context.Background(),
		handler,
		"investigate_service",
		map[string]any{"service_name": "workload:payments"},
		"Bearer scoped-token",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want service investigation envelope")
	}
	if result.IsError {
		t.Fatalf("dispatchTool() IsError = true, want false; envelope = %#v", result.Envelope)
	}
	if result.Envelope.Truth == nil || result.Envelope.Truth.Capability != "platform_impact.context_overview" {
		t.Fatalf("truth = %#v, want service investigation truth", result.Envelope.Truth)
	}
}
