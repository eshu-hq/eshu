// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// fakeOrphanGraph is an in-memory anti-join fixture shared by
// orphan_sweep_test.go, orphan_sweep_cycle_test.go,
// orphan_sweep_writeskip_test.go, orphan_sweep_race_test.go, and
// orphan_sweep_observer_test.go. It exercises the real
// SweepOrphanNodes/GraphOrphanNodeCounts production code (not a
// reimplementation of its logic) against scripted node/connectivity state by
// implementing OrphanSweepReader and Executor over an in-memory node table.

type fakeOrphanNode struct {
	observedAt *int64
	connected  bool
}

type fakeOrphanGraph struct {
	mu    sync.Mutex
	nodes map[string]map[string]*fakeOrphanNode // label -> key -> node
	execs []Statement
	// s2Calls counts BuildConnectedKeysQuery reads per label, so tests can
	// script a connectivity change that appears only starting on the Nth
	// read (used to prove the TOCTOU re-verify guard).
	s2Calls map[string]int
	// flipConnectedAfterS2Call maps "label:call_index" to the keys that
	// should become connected immediately after that S2 read returns.
	flipConnectedAfterS2Call map[string][]string
}

func newFakeOrphanGraph() *fakeOrphanGraph {
	return &fakeOrphanGraph{
		nodes:                    map[string]map[string]*fakeOrphanNode{},
		s2Calls:                  map[string]int{},
		flipConnectedAfterS2Call: map[string][]string{},
	}
}

func (g *fakeOrphanGraph) seed(label, key string, connected bool, observedAt *int64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.nodes[label] == nil {
		g.nodes[label] = map[string]*fakeOrphanNode{}
	}
	g.nodes[label][key] = &fakeOrphanNode{connected: connected, observedAt: observedAt}
}

func (g *fakeOrphanGraph) remaining(label string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.nodes[label])
}

func (g *fakeOrphanGraph) node(label, key string) (*fakeOrphanNode, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	n, ok := g.nodes[label][key]
	return n, ok
}

// flipAfterCall schedules keys to become connected right after the
// callIndex'th (1-based) BuildConnectedKeysQuery read for label.
func (g *fakeOrphanGraph) flipAfterCall(label string, callIndex int, keys ...string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.flipConnectedAfterS2Call[fmt.Sprintf("%s:%d", label, callIndex)] = keys
}

func fakeOrphanLabelFromCypher(cypher string) (string, bool) {
	for _, label := range []string{"Repository", "Platform", "EvidenceArtifact", "File", "Directory", "Module"} {
		if strings.Contains(cypher, "MATCH (n:"+label+")") || strings.Contains(cypher, "MATCH (n:"+label+" {") {
			return label, true
		}
	}
	return "", false
}

func (g *fakeOrphanGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	label, ok := fakeOrphanLabelFromCypher(cypher)
	if !ok {
		return nil, fmt.Errorf("fakeOrphanGraph.Run: no known label in cypher: %s", cypher)
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	nodes := g.nodes[label]

	if strings.Contains(cypher, "UNWIND $keys AS candidate_key") && strings.Contains(cypher, "-[r]-(m)") {
		// S2: connected-keys read.
		g.s2Calls[label]++
		keys, _ := params["keys"].([]string)
		rows := make([]map[string]any, 0, len(keys))
		for _, k := range keys {
			if n, ok := nodes[k]; ok && n.connected {
				rows = append(rows, map[string]any{"key": k})
			}
		}
		if flips, ok := g.flipConnectedAfterS2Call[fmt.Sprintf("%s:%d", label, g.s2Calls[label])]; ok {
			for _, k := range flips {
				if n, ok := nodes[k]; ok {
					n.connected = true
				}
			}
		}
		return rows, nil
	}

	// S1: candidates read.
	limit := 1 << 30
	if v, ok := params["limit"].(int); ok && v > 0 {
		limit = v
	}
	keys := make([]string, 0, len(nodes))
	for k := range nodes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	rows := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		if len(rows) >= limit {
			break
		}
		n := nodes[k]
		var observedAt any
		if n.observedAt != nil {
			observedAt = *n.observedAt
		}
		rows = append(rows, map[string]any{"key": k, "observed_at": observedAt})
	}
	return rows, nil
}

func (g *fakeOrphanGraph) Execute(_ context.Context, stmt Statement) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.execs = append(g.execs, stmt)

	label, ok := fakeOrphanLabelFromCypher(stmt.Cypher)
	if !ok {
		return fmt.Errorf("fakeOrphanGraph.Execute: no known label in cypher: %s", stmt.Cypher)
	}
	nodes := g.nodes[label]
	keys, _ := stmt.Parameters["keys"].([]string)

	switch {
	case strings.Contains(stmt.Cypher, "REMOVE n.eshu_orphan_observed_at_unix"):
		for _, k := range keys {
			if n, ok := nodes[k]; ok {
				n.observedAt = nil
			}
		}
	case strings.Contains(stmt.Cypher, "SET n.eshu_orphan_observed_at_unix"):
		ts, _ := stmt.Parameters["observed_at_unix"].(int64)
		for _, k := range keys {
			if n, ok := nodes[k]; ok {
				v := ts
				n.observedAt = &v
			}
		}
	case strings.Contains(stmt.Cypher, "DELETE n"):
		for _, k := range keys {
			delete(nodes, k)
		}
	default:
		return fmt.Errorf("fakeOrphanGraph.Execute: unrecognized write shape: %s", stmt.Cypher)
	}
	return nil
}

func int64Ptr(v int64) *int64 { return &v }

func orphanSweepTestTotal(values map[string]int64) int64 {
	var total int64
	for _, v := range values {
		total += v
	}
	return total
}
