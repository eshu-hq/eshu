// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"testing"
	"time"
)

func TestRelationshipVerbTilesSharesFourBreakdownSlotsAcrossRequests(t *testing.T) {
	t.Parallel()

	graph := &cappedBreakdownGraph{
		entered: make(chan struct{}, 2*len(relationshipVerbCatalog)),
		release: make(chan struct{}),
	}
	handler := &InfraHandler{Neo4j: graph, Profile: ProfileProduction}
	done := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := handler.relationshipVerbTiles(context.Background())
			done <- err
		}()
	}

	assertExactlyFourBreakdownsEnter(t, graph.entered)
	close(graph.release)
	for range 2 {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("relationshipVerbTiles() error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("shared-slot relationshipVerbTiles() call did not complete")
		}
	}

	// A completed request must return every permit to the handler-wide pool.
	reuseGraph := &cappedBreakdownGraph{
		entered: make(chan struct{}, len(relationshipVerbCatalog)),
		release: make(chan struct{}),
	}
	handler.Neo4j = reuseGraph
	reuseDone := make(chan error, 1)
	go func() {
		_, err := handler.relationshipVerbTiles(context.Background())
		reuseDone <- err
	}()
	assertExactlyFourBreakdownsEnter(t, reuseGraph.entered)
	close(reuseGraph.release)
	select {
	case err := <-reuseDone:
		if err != nil {
			t.Fatalf("relationshipVerbTiles() after permit reuse error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("relationshipVerbTiles() did not reuse released handler permits")
	}
}

func assertExactlyFourBreakdownsEnter(t *testing.T, entered <-chan struct{}) {
	t.Helper()
	for range relationshipBreakdownMaxConcurrency {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("four source-tool breakdowns did not enter the shared handler")
		}
	}
	select {
	case <-entered:
		t.Fatal("a fifth source-tool breakdown bypassed the shared handler cap")
	case <-time.After(25 * time.Millisecond):
	}
}
