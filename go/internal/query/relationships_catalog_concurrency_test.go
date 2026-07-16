// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

type aggregateBreakdownGraph struct {
	mu       sync.Mutex
	runCalls int
	err      error
}

func (*aggregateBreakdownGraph) RunSingle(
	_ context.Context,
	_ string,
	_ map[string]any,
) (map[string]any, error) {
	return map[string]any{"count": int64(1)}, nil
}

func (g *aggregateBreakdownGraph) Run(
	_ context.Context,
	cypher string,
	_ map[string]any,
) ([]map[string]any, error) {
	if !strings.Contains(cypher, "type(r) AS verb") ||
		!strings.Contains(cypher, "r.source_tool AS source_tool") {
		return nil, errors.New("unexpected non-aggregate breakdown query")
	}
	g.mu.Lock()
	g.runCalls++
	g.mu.Unlock()
	if g.err != nil {
		return nil, g.err
	}
	if strings.Contains(cypher, "MATCH (s:WorkloadInstance)") {
		return []map[string]any{{"verb": "RUNS_ON", "source_tool": "kubernetes", "count": int64(3)}}, nil
	}
	return []map[string]any{{"verb": "DEPENDS_ON", "source_tool": "helm", "count": int64(7)}}, nil
}

func TestRelationshipVerbTilesUsesOneAggregateReadPerSourceOwner(t *testing.T) {
	t.Parallel()

	graph := &aggregateBreakdownGraph{}
	handler := &InfraHandler{Neo4j: graph, Profile: ProfileProduction}
	tiles, err := handler.relationshipVerbTiles(context.Background())
	if err != nil {
		t.Fatalf("relationshipVerbTiles() error = %v", err)
	}
	if graph.runCalls != 2 {
		t.Fatalf("aggregate breakdown reads = %d, want 2 source owners", graph.runCalls)
	}
	for _, tile := range tiles {
		switch tile.Verb {
		case "DEPENDS_ON":
			if tile.SourceTools["helm"] != 7 {
				t.Fatalf("DEPENDS_ON source tools = %+v, want helm=7", tile.SourceTools)
			}
		case "RUNS_ON":
			if tile.SourceTools["kubernetes"] != 3 {
				t.Fatalf("RUNS_ON source tools = %+v, want kubernetes=3", tile.SourceTools)
			}
		}
	}
}

func TestRelationshipVerbTilesReturnsAggregateBreakdownError(t *testing.T) {
	t.Parallel()

	want := errors.New("aggregate breakdown failed")
	handler := &InfraHandler{
		Neo4j:   &aggregateBreakdownGraph{err: want},
		Profile: ProfileProduction,
	}
	_, err := handler.relationshipVerbTiles(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("relationshipVerbTiles() error = %v, want %v", err, want)
	}
}

type cappedBreakdownGraph struct {
	entered chan struct{}
	release chan struct{}
}

func (*cappedBreakdownGraph) RunSingle(
	_ context.Context,
	_ string,
	_ map[string]any,
) (map[string]any, error) {
	return map[string]any{"count": int64(1)}, nil
}

func (g *cappedBreakdownGraph) Run(
	ctx context.Context,
	_ string,
	_ map[string]any,
) ([]map[string]any, error) {
	select {
	case g.entered <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case <-g.release:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
