// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

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
	if err := startInfraSearchKeyRead(ctx, env("on"), infra, query.GraphBackendNeo4j, true, onlineProbeGraph{}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if infra.SearchKey == nil || !infra.SearchKey.Enabled || infra.SearchKey.Readiness == nil {
		t.Fatalf("driven Neo4j SearchKey = %+v, want enabled with readiness", infra.SearchKey)
	}
	// The watcher goroutine runs the first probe; a Start that built the
	// cache but never spawned Watch would leave it unready forever.
	deadline := time.Now().Add(5 * time.Second)
	for !infra.SearchKey.Readiness.Current().Ready {
		if time.Now().After(deadline) {
			t.Fatalf("driven Neo4j readiness = %+v after 5s, want Ready from the watcher's first probe", infra.SearchKey.Readiness.Current())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// onlineProbeGraph answers the readiness probes of a healthy store: the
// infra_search_key_ft index is ONLINE and no label holds an unkeyed node.
type onlineProbeGraph struct{}

func (onlineProbeGraph) Run(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
	switch {
	case strings.HasPrefix(cypher, "SHOW INDEXES"):
		return []map[string]any{{"state": "ONLINE"}}, nil
	case strings.Contains(cypher, "AS unkeyed"):
		return []map[string]any{{"unkeyed": int64(0)}}, nil
	}
	return nil, fmt.Errorf("unexpected statement: %s", cypher)
}

func (onlineProbeGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, errors.New("RunSingle is not used by the readiness probe")
}
