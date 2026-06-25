// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// Snapshot is the typed view of testdata/golden/e2e-20repo-snapshot.json, the
// B-12 golden contract that the B-7 corpus gate diffs a live run against. Only
// the fields the gate asserts on are modeled; unknown JSON keys are ignored so
// the snapshot can carry human-facing notes the gate does not consume.
type Snapshot struct {
	SchemaVersion string `json:"schema_version"`
	CorpusID      string `json:"corpus_id"`

	Graph           GraphSnapshot   `json:"graph"`
	DrainAssertions DrainAssertions `json:"drain_assertions"`
	QueryShapes     QueryShapes     `json:"query_shapes"`
}

// GraphSnapshot holds the per-label node and per-relationship edge count
// tolerances plus the existence-style required correlations.
type GraphSnapshot struct {
	NodeCounts           map[string]CountRange `json:"node_counts"`
	EdgeCounts           map[string]CountRange `json:"edge_counts"`
	RequiredCorrelations []RequiredCorrelation `json:"required_correlations"`
}

// CountRange is an inclusive [Min, Max] tolerance for a node label or edge type.
// The Note is human-facing and not asserted.
type CountRange struct {
	Min  int64  `json:"min"`
	Max  int64  `json:"max"`
	Note string `json:"note"`
}

// Contains reports whether n falls within the inclusive [Min, Max] range.
func (r CountRange) Contains(n int64) bool {
	return n >= r.Min && n <= r.Max
}

// RequiredCorrelation is an existence assertion: at least MinimumCount edges of
// Relationship must connect a From node to a To node. These hold regardless of
// corpus size and are the backbone of the minimal 5-repo gate (rc-1, rc-3).
type RequiredCorrelation struct {
	ID           string `json:"id"`
	Description  string `json:"description"`
	Relationship string `json:"relationship"`
	FromLabel    string `json:"from_label"`
	ToLabel      string `json:"to_label"`
	MinimumCount int64  `json:"minimum_count"`
}

// DrainAssertions captures the B-7(a) queue-drain gate: both queues must reach a
// terminal state before any graph or query truth is checked.
type DrainAssertions struct {
	FactWorkItems           DrainBound `json:"fact_work_items"`
	SharedProjectionIntents DrainBound `json:"shared_projection_intents"`
}

// DrainBound carries the maximum tolerated residual/nonterminal row count for a
// queue. The JSON uses distinct key names per queue (residual_max vs
// nonterminal_max); both unmarshal into Max here.
type DrainBound struct {
	ResidualMax    *int64 `json:"residual_max"`
	NonterminalMax *int64 `json:"nonterminal_max"`
	Note           string `json:"note"`
}

// Max returns the tolerated ceiling for the bound, preferring whichever JSON key
// was present. Absent both, it defaults to 0 (strict drain).
func (b DrainBound) Limit() int64 {
	switch {
	case b.ResidualMax != nil:
		return *b.ResidualMax
	case b.NonterminalMax != nil:
		return *b.NonterminalMax
	default:
		return 0
	}
}

// QueryShapes describes the canonical MCP and HTTP responses defined by the
// snapshot for B-7(c) query truth. The gate currently enforces only the HTTP
// shapes (checkQuery); the MCP shapes are modeled but not yet asserted —
// asserting them is tracked in the gate-widening follow-up (#3866). The field is
// kept so the snapshot stays the single source of truth and the follow-up only
// adds the assertion, not the schema.
type QueryShapes struct {
	MCP  map[string]QueryShape `json:"mcp"`
	HTTP map[string]QueryShape `json:"http"`
}

// QueryShape declares the required response fields and minimum result count for
// one canonical query. ResultItemRequiredFields, when set, are validated on each
// element of the first array-valued required field.
type QueryShape struct {
	Description              string   `json:"description"`
	RequiredResponseFields   []string `json:"required_response_fields"`
	MinimumResults           int      `json:"minimum_results"`
	ResultItemRequiredFields []string `json:"result_item_required_fields"`
}

// LoadSnapshot reads and parses the golden snapshot at path.
func LoadSnapshot(path string) (Snapshot, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // path is an operator-supplied gate input, not user data
	if err != nil {
		return Snapshot{}, fmt.Errorf("read snapshot %q: %w", path, err)
	}
	var snap Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return Snapshot{}, fmt.Errorf("parse snapshot %q: %w", path, err)
	}
	if snap.SchemaVersion == "" {
		return Snapshot{}, fmt.Errorf("snapshot %q: missing schema_version", path)
	}
	return snap, nil
}
