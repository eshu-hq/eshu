// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"log/slog"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/searchembedruntime"
)

// TestEntityHandlerReceivesGraphBackendInMCPWiring proves %s hands the
// parsed graph backend to the EntityHandler (#7380). Dropping the field would silently keep
// Neo4j on the 16-statement per-label entity-context loop; the dialect selector
// treats an unset backend as NornicDB. Neo4j is used because it differs from the
// unset default.
func TestEntityHandlerReceivesGraphBackendInMCPWiring(t *testing.T) {
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
	if router.Entities == nil {
		t.Fatal("router.Entities = nil, want mounted EntityHandler")
	}
	if got, want := router.Entities.GraphBackend, query.GraphBackendNeo4j; got != want {
		t.Fatalf("router.Entities.GraphBackend = %q, want %q (graphBackend not passed to EntityHandler in cmd/mcp-server wiring)", got, want)
	}
}
