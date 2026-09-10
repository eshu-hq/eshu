// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/searchembedruntime"
)

// TestLanguageQueryRouteMountedByMCPWiring proves the hoisted language-query
// mount (#6060 lane A) survived cmd/mcp-server wiring: LanguageQueryHandler
// lives in root package query and mounts from APIRouter.Mount via its own
// Language field, so deleting the Language entry from the router literal
// below would silently unmount POST /api/v0/code/language-query
// (mux.Handler reports an empty pattern for unmounted routes; a status-code
// probe cannot distinguish unmounted from erroring). The router is built
// with nil stores exactly as the wiring tolerates: construction performs no
// I/O, only assignment.
func TestLanguageQueryRouteMountedByMCPWiring(t *testing.T) {
	t.Parallel()

	router := newMCPQueryRouterWithSemanticEmbedding(
		nil, nil, nil, nil,
		query.ProfileLocalAuthoritative,
		query.GraphBackendNeo4j,
		slog.Default(),
		nil,
		searchembedruntime.Config{},
		"",
		component.Policy{},
		query.GovernanceStatusConfig{},
		nil,
		false,
	)
	mux := http.NewServeMux()
	router.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query", nil)
	_, pattern := mux.Handler(req)
	if pattern != "POST /api/v0/code/language-query" {
		t.Fatalf("mux pattern = %q, want POST /api/v0/code/language-query (Language entry missing from mcp-server wiring?)", pattern)
	}
}
