// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"log/slog"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/searchembedruntime"
)

// TestInfraHandlerReceivesGraphBackendInAPIWiring proves cmd/api hands the
// parsed graph backend to the InfraHandler (#7215). Dropping the field would
// silently revert scoped infra reads on Neo4j to the slow inline-map shape;
// the dialect selector treats an unset backend as NornicDB. Neo4j is used
// because it differs from the unset default.
func TestInfraHandlerReceivesGraphBackendInAPIWiring(t *testing.T) {
	t.Parallel()

	router, err := newRouterWithSemanticEmbedding(
		nil, nil, nil, nil, nil,
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
		"",
	)
	if err != nil {
		t.Fatalf("newRouterWithSemanticEmbedding() error = %v", err)
	}
	if router.Infra == nil {
		t.Fatal("router.Infra = nil, want mounted InfraHandler")
	}
	if got, want := router.Infra.GraphBackend, query.GraphBackendNeo4j; got != want {
		t.Fatalf("router.Infra.GraphBackend = %q, want %q (graphBackend not passed to InfraHandler in cmd/api wiring)", got, want)
	}
}

// TestStartInfraSearchKeyReadWiresTheReader proves cmd/api hands the #7245
// full-text reader to the InfraHandler, refuses an invalid flag, and starts
// readiness only on a driven Neo4j graph.
func TestStartInfraSearchKeyReadWiresTheReader(t *testing.T) {
	t.Parallel()
	env := func(v string) func(string) string {
		return func(key string) string {
			if key == "ESHU_INFRA_SEARCH_KEY_READ" {
				return v
			}
			return ""
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	infra := &query.InfraHandler{}
	if err := startInfraSearchKeyRead(ctx, env("yes"), infra, query.GraphBackendNeo4j, true, nil, nil, nil); err == nil {
		t.Fatal("invalid ESHU_INFRA_SEARCH_KEY_READ accepted")
	}
	if err := startInfraSearchKeyRead(ctx, env("on"), infra, query.GraphBackendNornicDB, true, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if infra.SearchKey == nil || !infra.SearchKey.Enabled || infra.SearchKey.Readiness != nil {
		t.Fatalf("NornicDB SearchKey = %+v, want enabled with no readiness", infra.SearchKey)
	}
	if err := startInfraSearchKeyRead(ctx, env(""), infra, query.GraphBackendNeo4j, false, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if infra.SearchKey == nil || infra.SearchKey.Enabled || infra.SearchKey.Readiness != nil {
		t.Fatalf("graph-free SearchKey = %+v, want disabled with no readiness", infra.SearchKey)
	}
}
