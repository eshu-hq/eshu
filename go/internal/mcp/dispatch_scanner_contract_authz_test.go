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

func TestDispatchToolScannerContractAllowsScopedRoute(t *testing.T) {
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
		if got, want := r.Method, http.MethodGet; got != want {
			t.Fatalf("method = %q, want %q", got, want)
		}
		if got, want := r.URL.Path, "/api/v0/supply-chain/vulnerability-scanner/contract"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get("route"), "impact_findings"; got != want {
			t.Fatalf("route query = %q, want %q", got, want)
		}
		if _, ok := query.AuthContextFromContext(r.Context()); !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		query.WriteSuccess(w, r, http.StatusOK, map[string]any{
			"schema_version": "eshu.vulnerability_scanner_read_contract.v1",
		}, query.BuildTruthEnvelope(
			query.ProfileLocalAuthoritative,
			"supply_chain.vulnerability_scanner.contract.read",
			query.TruthBasisContentIndex,
			"static API/MCP scanner read contract; no live backend read",
		))
	}))

	result, err := dispatchTool(
		context.Background(),
		handler,
		"get_vulnerability_scanner_read_contract",
		map[string]any{"route": "impact_findings"},
		"Bearer scoped-token",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want scanner contract envelope")
	}
	if result.IsError {
		t.Fatalf("dispatchTool() IsError = true, want false; envelope = %#v", result.Envelope)
	}
	if result.Envelope.Truth == nil || result.Envelope.Truth.Capability != "supply_chain.vulnerability_scanner.contract.read" {
		t.Fatalf("truth = %#v, want scanner contract truth", result.Envelope.Truth)
	}
}
