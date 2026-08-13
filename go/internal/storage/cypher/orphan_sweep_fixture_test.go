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

// seed stores one node under its identity key. A label whose identity has more
// properties than the caller supplied is padded with empty values, which is
// what a real node with the property absent decodes to, so the many existing
// single-key tests keep meaning what they meant.
func (g *fakeOrphanGraph) seed(label, key string, connected bool, observedAt *int64) {
	values := decodeOrphanSweepKey(key)
	properties, ok := orphanSweepIdentityProperties(OrphanSweepLabel(label))
	if ok {
		for len(values) < len(properties) {
			values = append(values, "")
		}
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.nodes[label] == nil {
		g.nodes[label] = map[string]*fakeOrphanNode{}
	}
	g.nodes[label][encodeOrphanSweepKey(values)] = &fakeOrphanNode{connected: connected, observedAt: observedAt}
}

// seedComposite seeds a node for a label whose identity is more than one
// property (Module: name plus lang). The fixture stores nodes under the same
// encoded form the sweep uses internally, so a Go `time` and a Python `time`
// are two distinct rows here exactly as they are two distinct nodes in the
// graph.
func (g *fakeOrphanGraph) seedComposite(label string, values []string, connected bool, observedAt *int64) {
	g.seed(label, encodeOrphanSweepKey(values), connected, observedAt)
}

// compositeKeyRows renders the fixture's stored nodes for a label back into
// (property values) tuples, so a test can assert which rows survived a sweep
// without reaching into the encoding.
func (g *fakeOrphanGraph) compositeKeyRows(label string) [][]string {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([][]string, 0, len(g.nodes[label]))
	for encoded := range g.nodes[label] {
		out = append(out, decodeOrphanSweepKey(encoded))
	}
	sort.Slice(out, func(i, j int) bool {
		return encodeOrphanSweepKey(out[i]) < encodeOrphanSweepKey(out[j])
	})
	return out
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
		keys := fakeOrphanParamKeys(params)
		rows := make([]map[string]any, 0, len(keys))
		for _, k := range keys {
			if n, ok := nodes[k]; ok && n.connected {
				rows = append(rows, fakeOrphanKeyRow(label, k))
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

	// S1: candidates read. Honors the paging cursor and the ORDER BY + LIMIT
	// the real query uses, so cursor advancement is exercised faithfully. For a
	// composite-key label the cursor is a tuple, compared the same way the
	// emitted Cypher compares it: strictly greater on the leading property, or
	// equal there and strictly greater on the next.
	limit := 1 << 30
	if v, ok := params["limit"].(int); ok && v > 0 {
		limit = v
	}
	cursor := fakeOrphanParamCursor(params)
	keys := make([]string, 0, len(nodes))
	for k := range nodes {
		if k > cursor {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	rows := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		if len(rows) >= limit {
			break
		}
		n := nodes[k]
		row := fakeOrphanKeyRow(label, k)
		if n.observedAt != nil {
			row["observed_at"] = *n.observedAt
		} else {
			row["observed_at"] = nil
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// fakeOrphanParamKeys reads the $keys parameter in either shape: a plain
// string list for a single-property label, or a list of {key_0, key_1, ...}
// maps for a composite one. It returns the encoded form the fixture stores.
func fakeOrphanParamKeys(params map[string]any) []string {
	if plain, ok := params["keys"].([]string); ok {
		return plain
	}
	rows, _ := params["keys"].([]map[string]any)
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		values := make([]string, 0, len(row))
		for i := 0; i < len(row); i++ {
			value, ok := row[fmt.Sprintf("key_%d", i)].(string)
			if !ok {
				break
			}
			values = append(values, value)
		}
		out = append(out, encodeOrphanSweepKey(values))
	}
	return out
}

// fakeOrphanParamCursor rebuilds the encoded cursor from the emitted
// parameters: $cursor for a single-property label, $cursor_0/$cursor_1/... for
// a composite one.
func fakeOrphanParamCursor(params map[string]any) string {
	if plain, ok := params["cursor"].(string); ok {
		return plain
	}
	values := make([]string, 0, 2)
	for i := 0; ; i++ {
		value, ok := params[fmt.Sprintf("cursor_%d", i)].(string)
		if !ok {
			break
		}
		values = append(values, value)
	}
	if len(values) == 0 {
		return ""
	}
	return encodeOrphanSweepKey(values)
}

// fakeOrphanKeyRow renders one encoded key back into the row shape the real
// read returns for this label: {key} for a single-property identity,
// {key_0, key_1, ...} for a composite one.
func fakeOrphanKeyRow(label, encoded string) map[string]any {
	values := decodeOrphanSweepKey(encoded)
	properties, ok := orphanSweepIdentityProperties(OrphanSweepLabel(label))
	if ok && len(properties) == 1 {
		return map[string]any{"key": values[0]}
	}
	row := make(map[string]any, len(values))
	for i, value := range values {
		row[fmt.Sprintf("key_%d", i)] = value
	}
	return row
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
	keys := fakeOrphanParamKeys(stmt.Parameters)

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

// singleOrphanKeys lifts plain string keys into the identity-key type used by
// the sweep, for the single-property labels whose identity is one value.
func singleOrphanKeys(values []string) []orphanSweepKey {
	out := make([]orphanSweepKey, 0, len(values))
	for _, value := range values {
		out = append(out, orphanSweepKey{value})
	}
	return out
}

// flattenOrphanKeys is the inverse of singleOrphanKeys, for assertions written
// against plain strings.
func flattenOrphanKeys(keys []orphanSweepKey) []string {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key.encode())
	}
	return out
}

func orphanSweepTestTotal(values map[string]int64) int64 {
	var total int64
	for _, v := range values {
		total += v
	}
	return total
}
