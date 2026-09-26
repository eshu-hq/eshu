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

// TestInfraHandlerReceivesGraphBackendInMCPWiring proves cmd/mcp-server hands
// the parsed graph backend to the InfraHandler (#7215). Dropping the field
// would silently revert scoped infra reads on Neo4j to the slow inline-map
// shape; the dialect selector treats an unset backend as NornicDB. Neo4j is
// used because it differs from the unset default.
func TestInfraHandlerReceivesGraphBackendInMCPWiring(t *testing.T) {
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
	if router.Infra == nil {
		t.Fatal("router.Infra = nil, want mounted InfraHandler")
	}
	if got, want := router.Infra.GraphBackend, query.GraphBackendNeo4j; got != want {
		t.Fatalf("router.Infra.GraphBackend = %q, want %q (graphBackend not passed to InfraHandler in cmd/mcp-server wiring)", got, want)
	}
}
