// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type overlappingBreakdownGraph struct {
	wantConcurrent int

	mu      sync.Mutex
	started int
	release chan struct{}
}

func newOverlappingBreakdownGraph() *overlappingBreakdownGraph {
	want := 0
	for _, entry := range relationshipVerbCatalog {
		if entry.carriesSourceTool {
			want++
		}
	}
	return &overlappingBreakdownGraph{
		wantConcurrent: want,
		release:        make(chan struct{}),
	}
}

func (*overlappingBreakdownGraph) RunSingle(
	_ context.Context,
	_ string,
	_ map[string]any,
) (map[string]any, error) {
	return map[string]any{"count": int64(1)}, nil
}

func (g *overlappingBreakdownGraph) Run(
	ctx context.Context,
	cypher string,
	_ map[string]any,
) ([]map[string]any, error) {
	if !strings.Contains(cypher, "source_tool IS NOT NULL") {
		return nil, fmt.Errorf("unexpected non-breakdown query: %s", cypher)
	}

	g.mu.Lock()
	g.started++
	if g.started == g.wantConcurrent {
		close(g.release)
	}
	g.mu.Unlock()

	select {
	case <-g.release:
		return []map[string]any{{"source_tool": "test", "count": int64(1)}}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestRelationshipVerbTilesOverlapsIndependentSourceToolBreakdowns(t *testing.T) {
	t.Parallel()

	graph := newOverlappingBreakdownGraph()
	handler := &InfraHandler{Neo4j: graph, Profile: ProfileProduction}
	type result struct {
		tiles []relationshipVerbTile
		err   error
	}
	done := make(chan result, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		tiles, err := handler.relationshipVerbTiles(ctx)
		done <- result{tiles: tiles, err: err}
	}()

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("relationshipVerbTiles() error = %v", got.err)
		}
		if len(got.tiles) != len(relationshipVerbCatalog) {
			t.Fatalf("tiles = %d, want %d", len(got.tiles), len(relationshipVerbCatalog))
		}
		for i, tile := range got.tiles {
			if tile.Verb != relationshipVerbCatalog[i].verb {
				t.Fatalf("tile[%d].Verb = %q, want %q", i, tile.Verb, relationshipVerbCatalog[i].verb)
			}
		}
	case <-ctx.Done():
		t.Fatal("source-tool breakdowns did not overlap before the test deadline")
	}
}

type failingBreakdownGraph struct {
	*overlappingBreakdownGraph
}

func (g *failingBreakdownGraph) Run(
	ctx context.Context,
	cypher string,
	_ map[string]any,
) ([]map[string]any, error) {
	g.mu.Lock()
	g.started++
	if g.started == g.wantConcurrent {
		close(g.release)
	}
	g.mu.Unlock()

	select {
	case <-g.release:
		return nil, fmt.Errorf("%s breakdown failed", verbInCypher(cypher))
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestRelationshipVerbTilesReturnsBreakdownErrorInCatalogOrder(t *testing.T) {
	t.Parallel()

	graph := &failingBreakdownGraph{overlappingBreakdownGraph: newOverlappingBreakdownGraph()}
	handler := &InfraHandler{Neo4j: graph, Profile: ProfileProduction}
	_, err := handler.relationshipVerbTiles(context.Background())
	if err == nil {
		t.Fatal("relationshipVerbTiles() error = nil, want first catalog breakdown error")
	}
	if !strings.Contains(err.Error(), "DEPLOYS_FROM breakdown failed") {
		t.Fatalf("relationshipVerbTiles() error = %q, want deterministic DEPLOYS_FROM error", err)
	}
}

type owningBreakdownErrorGraph struct {
	failed chan struct{}
}

func (*owningBreakdownErrorGraph) RunSingle(
	_ context.Context,
	_ string,
	_ map[string]any,
) (map[string]any, error) {
	return map[string]any{"count": int64(1)}, nil
}

func (g *owningBreakdownErrorGraph) Run(
	ctx context.Context,
	cypher string,
	_ map[string]any,
) ([]map[string]any, error) {
	switch verbInCypher(cypher) {
	case "DEPLOYS_FROM":
		<-g.failed
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
			return nil, nil
		}
	case "USES_MODULE":
		close(g.failed)
		return nil, fmt.Errorf("USES_MODULE owning failure")
	default:
		return nil, nil
	}
}

func TestRelationshipVerbTilesDoesNotMaskOwningErrorWithInternalCancellation(t *testing.T) {
	t.Parallel()

	handler := &InfraHandler{
		Neo4j:   &owningBreakdownErrorGraph{failed: make(chan struct{})},
		Profile: ProfileProduction,
	}
	_, err := handler.relationshipVerbTiles(context.Background())
	if err == nil {
		t.Fatal("relationshipVerbTiles() error = nil, want owning breakdown error")
	}
	if !strings.Contains(err.Error(), "USES_MODULE owning failure") {
		t.Fatalf("relationshipVerbTiles() error = %q, want owning failure instead of internal cancellation", err)
	}
}
