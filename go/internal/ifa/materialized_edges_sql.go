// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// sqlRelationshipExpectedEdgesRelPath and sqlRelationshipDeltaExpectedEdgesRelPath
// are repo-root-relative paths to the hand-derived expected SQL relationship
// edge sets (#5351). They live under this package's own testdata/ tree, NOT
// under testdata/cassettes/: they are Ifá gate ASSERTION files (fields
// edges/odu/note), not replay cassettes (which require schema_version +
// scopes), and the offline cassette validator globs every
// testdata/cassettes/*/*.json as a cassette (internal/replay/schema's
// TestCommittedCassettesValid). The live-drive cassettes they describe
// (ifa-sql-family.json + the delta) ARE valid cassettes and stay under
// testdata/cassettes/sqlrelationships/. These assertion files are loaded
// directly by this package's pure vacuity guard and by `ifa assert-edges` —
// never captured from a live run (that would make the gate a snapshot test,
// not an exhaustiveness proof).
const (
	sqlRelationshipExpectedEdgesRelPath      = "go/internal/ifa/testdata/sqlrelationships/ifa-sql-family-expected-edges.json"
	sqlRelationshipDeltaExpectedEdgesRelPath = "go/internal/ifa/testdata/sqlrelationships/ifa-sql-family-delta-expected-edges.json"
)

// sqlFamilyExpectedEdgesPath joins repoRoot onto the v1 Odù's expected-edge-set
// fixture path.
func sqlFamilyExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, sqlRelationshipExpectedEdgesRelPath)
}

// sqlFamilyDeltaExpectedEdgesPath joins repoRoot onto the gen-2 delta Odù's
// expected-edge-set fixture path.
func sqlFamilyDeltaExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, sqlRelationshipDeltaExpectedEdgesRelPath)
}

// sqlRelationshipExpectedEdge is one hand-derived expected SQL relationship
// edge: the identity triple the #5351 vacuity guard asserts, deliberately
// excluding source_path (production content_entity facts never carry a
// top-level "path" key — see sql_relationship_odu.go's doc comment — so
// source_path is not part of any edge's identity here).
type sqlRelationshipExpectedEdge struct {
	RelationshipType string `json:"relationship_type"`
	SourceEntityID   string `json:"source_entity_id"`
	TargetEntityID   string `json:"target_entity_id"`
}

type sqlRelationshipExpectedEdgesFile struct {
	Odu   string                        `json:"odu"`
	Edges []sqlRelationshipExpectedEdge `json:"edges"`
}

// loadSQLRelationshipExpectedEdges reads and parses one hand-derived
// expected-edge-set fixture file.
func loadSQLRelationshipExpectedEdges(path string) ([]sqlRelationshipExpectedEdge, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is a checked-in repo fixture under testdata/, not external input
	if err != nil {
		return nil, fmt.Errorf("ifa: read sql relationship expected edges %s: %w", path, err)
	}
	var parsed sqlRelationshipExpectedEdgesFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("ifa: parse sql relationship expected edges %s: %w", path, err)
	}
	if len(parsed.Edges) == 0 {
		return nil, fmt.Errorf("ifa: sql relationship expected edges %s has no edges", path)
	}
	return parsed.Edges, nil
}

// sqlRelationshipEdgeKey builds the canonical set-membership key for one
// expected or derived edge.
func sqlRelationshipEdgeKey(relationshipType, sourceEntityID, targetEntityID string) string {
	return relationshipType + "|" + sourceEntityID + "|" + targetEntityID
}

// sqlRelationshipEdgeSet builds a set keyed by sqlRelationshipEdgeKey so exact
// set-equality (not just subset) can be asserted between the expected and
// derived edges: a duplicate in either slice collapses to one key, matching
// ExtractSQLRelationshipRows's own seenEdges dedup contract.
func sqlRelationshipEdgeSet(edges []sqlRelationshipExpectedEdge) map[string]struct{} {
	out := make(map[string]struct{}, len(edges))
	for _, e := range edges {
		out[sqlRelationshipEdgeKey(e.RelationshipType, e.SourceEntityID, e.TargetEntityID)] = struct{}{}
	}
	return out
}

// sqlRelationshipRowsToExpectedEdges adapts reducer.ExtractSQLRelationshipRows's
// []map[string]any row shape into the same typed identity triple the
// hand-derived expected set uses, so both sides compare through one shared
// set-equality helper.
func sqlRelationshipRowsToExpectedEdges(rows []map[string]any) []sqlRelationshipExpectedEdge {
	out := make([]sqlRelationshipExpectedEdge, 0, len(rows))
	for _, row := range rows {
		out = append(out, sqlRelationshipExpectedEdge{
			RelationshipType: anyToStringValue(row["relationship_type"]),
			SourceEntityID:   anyToStringValue(row["source_entity_id"]),
			TargetEntityID:   anyToStringValue(row["target_entity_id"]),
		})
	}
	return out
}

// anyToStringValue extracts a string from an untyped map value, mirroring the
// reducer package's own anyToString helper without importing its unexported
// symbol.
func anyToStringValue(v any) string {
	s, _ := v.(string)
	return s
}

// resolveSQLRelationshipMaterializedEdges is the materialized_edges:
// sql_relationships vacuity guard (#5351 design §1's "green condition is NOT
// name-binding alone"). It resolves true iff all three hold:
//
//  1. odu is cataloged with the odu scenario (checked by the caller before
//     this is reached).
//  2. The hand-derived expected-edge-set file exists, parses, and names at
//     least one edge of EVERY relationship type
//     cypher.SQLRelationshipMaterializedEdgeTypes() accepts — the
//     registry-driven exhaustiveness half: an 8th writer type added later
//     with no matching expected-set entry flips this red.
//  3. Running odu's own facts through the pure, backend-free
//     reducer.ExtractSQLRelationshipRows seam reproduces the expected set
//     EXACTLY (same count, same identity triples) — the vacuity half: a
//     fixture that merely LOOKS right (right Odù name bound) but whose facts
//     don't actually derive the claimed edges cannot pass.
func resolveSQLRelationshipMaterializedEdges(odu Odu, expectedEdgesPath string) (bool, string) {
	expected, err := loadSQLRelationshipExpectedEdges(expectedEdgesPath)
	if err != nil {
		return false, err.Error()
	}

	registry := cypher.SQLRelationshipMaterializedEdgeTypes()
	seenTypes := make(map[string]struct{}, len(expected))
	for _, e := range expected {
		seenTypes[e.RelationshipType] = struct{}{}
	}
	var missingTypes []string
	for edgeType := range registry {
		if _, ok := seenTypes[edgeType]; !ok {
			missingTypes = append(missingTypes, edgeType)
		}
	}
	if len(missingTypes) > 0 {
		return false, fmt.Sprintf("odù %q: expected-edge-set %s does not cover every registry edge type, missing: %v", odu.Name, expectedEdgesPath, missingTypes)
	}

	expectedSet := sqlRelationshipEdgeSet(expected)
	_, rows, _ := reducer.ExtractSQLRelationshipRows(odu.Facts)
	actual := sqlRelationshipRowsToExpectedEdges(rows)
	actualSet := sqlRelationshipEdgeSet(actual)

	if len(actualSet) != len(expectedSet) {
		return false, fmt.Sprintf("odù %q: ExtractSQLRelationshipRows produced %d distinct edges, expected-edge-set names %d", odu.Name, len(actualSet), len(expectedSet))
	}
	for key := range expectedSet {
		if _, ok := actualSet[key]; !ok {
			return false, fmt.Sprintf("odù %q: expected edge %s not reproduced by ExtractSQLRelationshipRows", odu.Name, key)
		}
	}
	for key := range actualSet {
		if _, ok := expectedSet[key]; !ok {
			return false, fmt.Sprintf("odù %q: ExtractSQLRelationshipRows produced unexpected edge %s not in the expected-edge-set", odu.Name, key)
		}
	}

	return true, fmt.Sprintf("odù %q: ExtractSQLRelationshipRows reproduces the expected %d-edge set exactly, covering all %d registry types", odu.Name, len(expectedSet), len(registry))
}
