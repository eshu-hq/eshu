// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer/sqlrelationship"
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
	sqlRelationshipExpectedEdgesRelPath          = "go/internal/ifa/testdata/sqlrelationships/ifa-sql-family-expected-edges.json"
	sqlRelationshipDeltaExpectedEdgesRelPath     = "go/internal/ifa/testdata/sqlrelationships/ifa-sql-family-delta-expected-edges.json"
	sqlRelationshipDeltaLiveExpectedEdgesRelPath = "go/internal/ifa/testdata/sqlrelationships/ifa-sql-family-delta-live-expected-edges.json"
)

// sqlFamilyOduName and sqlFamilyDeltaOduName duplicate the SQL family's Odù
// names from ifa's sql_relationship_odu.go: this package cannot reach those
// unexported constants across the package boundary, and sql_relationship_odu.go
// still needs its own copy to stay in ifa (it seeds the compiled catalog), so
// this is a copy, not a move.
const (
	sqlFamilyOduName      = "odu:ifa-sql-family"
	sqlFamilyDeltaOduName = "odu:ifa-sql-family-delta"
)

// The SQL family's local_path and generation ID are read from ifa rather than
// copied here (#6053). They sit on the REFERENCE side of the sibling-identity
// collision assertions in this package, and a copied reference silently stops
// detecting the collision it exists to catch: freeze the value here and a real
// collision introduced in ifa compares equal to the stale literal and passes.
// Whether any other copied identity is safe is not asserted here. Successive
// review passes each found one more that was not, including one whose extractor
// stamps the value through without filtering on it. Re-derive it by mutation --
// mutate a copy two ways and run the package's tests: append "-STALE", and
// re-point it at a value that already exists in the corpus. A suffix alone only
// probes absence, so for a path it always reds and always answers "safe".

// repositoryFactKind and contentEntityFactKind duplicate the raw fact-kind
// literals from ifa's catalog_seed.go for the same cross-boundary reason:
// evidence.go, repo_dependency_odu.go, sql_relationship_odu.go, and
// catalog_seed.go itself all still need their own copies.
const (
	repositoryFactKind    = "repository"
	contentEntityFactKind = "content_entity"
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

// sqlFamilyDeltaLiveExpectedEdgesPath joins repoRoot onto the accumulated
// gen-1 + gen-2 live expected-edge-set fixture path.
func sqlFamilyDeltaLiveExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, sqlRelationshipDeltaLiveExpectedEdgesRelPath)
}

// sqlRelationshipExpectedEdge is one hand-derived expected SQL relationship
// edge: the identity triple the #5351 vacuity guard asserts, deliberately
// excluding source_path (production content_entity facts never carry a
// top-level "path" key — see sql_relationship_odu.go's doc comment — so
// source_path is not part of any edge's identity here). Field order and
// types MUST stay identical to ExpectedEdge (materialized_edges_assert.go):
// LoadExpectedEdges converts directly between the two structs, and the
// compiler rejects that conversion the moment their shapes diverge.
type sqlRelationshipExpectedEdge struct {
	RelationshipType string            `json:"relationship_type"`
	SourceEntityID   string            `json:"source_entity_id"`
	TargetEntityID   string            `json:"target_entity_id"`
	Identity         map[string]string `json:"identity,omitempty"`
	Properties       map[string]string `json:"properties,omitempty"`
}

type sqlRelationshipExpectedEdgesFile struct {
	Odu string `json:"odu"`
	// Note is free-form authoring context every committed expected-edge-set
	// fixture carries; it is modeled here (rather than left to strict-decode
	// rejection) purely so loadSQLRelationshipExpectedEdges's
	// DisallowUnknownFields decoder does not reject every existing fixture.
	Note  string                        `json:"note,omitempty"`
	Edges []sqlRelationshipExpectedEdge `json:"edges"`
}

// loadSQLRelationshipExpectedEdges reads and parses one hand-derived
// expected-edge-set fixture file. Despite its name (kept for the SQL family
// that introduced it -- renaming would ripple across a dozen-plus call sites
// and comments in this package's tests, not "a few"), this is the SHARED
// loader every family's guard calls through LoadExpectedEdges
// (materialized_edges_assert.go:140), including handles_route/runs_in/
// invokes_cloud_action. Its own error strings are therefore family-neutral
// ("expected edges", never "sql relationship expected edges"): a mutation-test
// pass on the symbol-runtime trio found the old wording surfacing as "sql
// relationship expected edges ... has no edges" while debugging an
// invokes_cloud_action fixture, which is the wrong artifact name at exactly
// the moment a reader needs the right one.
//
// The decoder disallows unknown fields so a typo in a fixture (e.g. "identiy"
// instead of "identity") fails loudly at load time instead of silently
// decoding to a zero value.
//
// json.Decoder.Decode reads exactly one JSON value off the stream and
// stops -- unlike json.Unmarshal, it does not require the input to end
// there, so a fixture holding a second, concatenated JSON value after a
// valid first one would decode only the first and silently drop the rest.
// Requiring io.EOF from a second Decode call closes that gap: any trailing
// non-whitespace content after the first value is a fixture error, not a
// value this loader ignores.
func loadSQLRelationshipExpectedEdges(path string) ([]sqlRelationshipExpectedEdge, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is a checked-in repo fixture under testdata/, not external input
	if err != nil {
		return nil, fmt.Errorf("ifa: read expected edges %s: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var parsed sqlRelationshipExpectedEdgesFile
	if err := decoder.Decode(&parsed); err != nil {
		return nil, fmt.Errorf("ifa: parse expected edges %s: %w", path, err)
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("ifa: expected edges %s has trailing content after its JSON object", path)
	}
	if len(parsed.Edges) == 0 {
		return nil, fmt.Errorf("ifa: expected edges %s has no edges", path)
	}
	return parsed.Edges, nil
}

// sqlRelationshipEdgeKey builds the canonical set-membership key for one
// expected or derived edge, using the same length-prefixed
// ("<byte length>:<content>") encoding as ExpectedEdge.Key()'s
// Identity-bearing path (writeLengthPrefixedField,
// materialized_edges_assert.go) rather than a raw "|"-joined string. A raw
// join is not injective: nothing stops relationshipType, sourceEntityID, or
// targetEntityID from containing "|" itself, so two distinct edges could
// render the identical key (see TestSQLRelationshipEdgeKeyIsInjective).
// ExpectedEdge.Key()'s empty-Identity path delegates here, so fixing this one
// function closes the gap for every family with no declared identity at
// once, keeping exactly one encoding for all of Key()'s paths instead of
// two.
func sqlRelationshipEdgeKey(relationshipType, sourceEntityID, targetEntityID string) string {
	var b strings.Builder
	writeLengthPrefixedField(&b, relationshipType)
	writeLengthPrefixedField(&b, sourceEntityID)
	writeLengthPrefixedField(&b, targetEntityID)
	return b.String()
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

// sqlRelationshipRowsToExpectedEdges adapts sqlrelationship.ExtractSQLRelationshipRows's
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
//     sqlrelationship.ExtractSQLRelationshipRows seam reproduces the expected set
//     EXACTLY (same count, same identity triples) — the vacuity half: a
//     fixture that merely LOOKS right (right Odù name bound) but whose facts
//     don't actually derive the claimed edges cannot pass.
func resolveSQLRelationshipMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, err := loadSQLRelationshipExpectedEdges(expectedEdgesPath)
	if err != nil {
		return false, err.Error()
	}

	registry := cypher.SQLRelationshipMaterializedEdgeTypes()
	if missingTypes := missingSQLRelationshipExpectedTypes(expected, registry); len(missingTypes) > 0 {
		return false, fmt.Sprintf("odù %q: expected-edge-set %s does not cover every registry edge type, missing: %v", odu.Name, expectedEdgesPath, missingTypes)
	}

	_, rows, _ := sqlrelationship.ExtractSQLRelationshipRows(odu.Facts)
	actual := sqlRelationshipRowsToExpectedEdges(rows)
	if mismatch := compareSQLRelationshipExpectedSets(odu.Name, expected, actual); mismatch != "" {
		return false, mismatch
	}

	return true, fmt.Sprintf("odù %q: ExtractSQLRelationshipRows reproduces the expected %d-edge set exactly, covering all %d registry types", odu.Name, len(expected), len(registry))
}

// resolveSQLRelationshipDeltaMaterializedEdges derives the accumulated live
// graph from the baseline and delta Odùs: baseline rows sourced from a changed
// file are retracted, unchanged-file rows survive, and delta rows are added.
// That exact pure set must match the same fixture the live matrix asserts.
func resolveSQLRelationshipDeltaMaterializedEdges(
	baseline ifa.Odu,
	delta ifa.Odu,
	expectedEdgesPath string,
) (bool, string) {
	expected, err := loadSQLRelationshipExpectedEdges(expectedEdgesPath)
	if err != nil {
		return false, err.Error()
	}
	registry := cypher.SQLRelationshipMaterializedEdgeTypes()
	if missingTypes := missingSQLRelationshipExpectedTypes(expected, registry); len(missingTypes) > 0 {
		return false, fmt.Sprintf("odù %q: delta-live expected-edge-set %s does not cover every registry edge type, missing: %v", delta.Name, expectedEdgesPath, missingTypes)
	}

	_, baselineRows, _ := sqlrelationship.ExtractSQLRelationshipRows(baseline.Facts)
	_, deltaRows, _ := sqlrelationship.ExtractSQLRelationshipRows(delta.Facts)
	changedPaths := sqlRelationshipDeltaRelativePaths(delta)
	if len(changedPaths) == 0 {
		return false, fmt.Sprintf("odù %q: repository fact identifies no delta_relative_paths", delta.Name)
	}
	baselineEntityPaths := sqlRelationshipEntityRelativePaths(baseline)

	accumulatedRows := make([]map[string]any, 0, len(baselineRows)+len(deltaRows))
	for _, row := range baselineRows {
		sourceEntityID := anyToStringValue(row["source_entity_id"])
		if _, changed := changedPaths[baselineEntityPaths[sourceEntityID]]; changed {
			continue
		}
		accumulatedRows = append(accumulatedRows, row)
	}
	accumulatedRows = append(accumulatedRows, deltaRows...)
	actual := sqlRelationshipRowsToExpectedEdges(accumulatedRows)
	if mismatch := compareSQLRelationshipExpectedSets(delta.Name+" accumulated", expected, actual); mismatch != "" {
		return false, mismatch
	}

	return true, fmt.Sprintf("odù %q: baseline + delta derivation reproduces the expected accumulated %d-edge set exactly, covering all %d registry types", delta.Name, len(expected), len(registry))
}

func sqlRelationshipDeltaRelativePaths(odu ifa.Odu) map[string]struct{} {
	paths := make(map[string]struct{})
	for _, envelope := range odu.Facts {
		if envelope.FactKind != repositoryFactKind {
			continue
		}
		switch values := envelope.Payload["delta_relative_paths"].(type) {
		case []string:
			for _, value := range values {
				if value = strings.TrimSpace(value); value != "" {
					paths[value] = struct{}{}
				}
			}
		case []any:
			for _, raw := range values {
				if value := strings.TrimSpace(anyToStringValue(raw)); value != "" {
					paths[value] = struct{}{}
				}
			}
		}
	}
	return paths
}

func sqlRelationshipEntityRelativePaths(odu ifa.Odu) map[string]string {
	paths := make(map[string]string)
	for _, envelope := range odu.Facts {
		if envelope.FactKind != contentEntityFactKind {
			continue
		}
		entityID := strings.TrimSpace(anyToStringValue(envelope.Payload["entity_id"]))
		relativePath := strings.TrimSpace(anyToStringValue(envelope.Payload["relative_path"]))
		if entityID != "" && relativePath != "" {
			paths[entityID] = relativePath
		}
	}
	return paths
}

func missingSQLRelationshipExpectedTypes(
	expected []sqlRelationshipExpectedEdge,
	registry map[string]string,
) []string {
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
	sort.Strings(missingTypes)
	return missingTypes
}

func compareSQLRelationshipExpectedSets(
	label string,
	expected []sqlRelationshipExpectedEdge,
	actual []sqlRelationshipExpectedEdge,
) string {
	expectedSet := sqlRelationshipEdgeSet(expected)
	actualSet := sqlRelationshipEdgeSet(actual)

	if len(actualSet) != len(expectedSet) {
		return fmt.Sprintf("odù %q: derived %d distinct edges, expected-edge-set names %d", label, len(actualSet), len(expectedSet))
	}
	for key := range expectedSet {
		if _, ok := actualSet[key]; !ok {
			return fmt.Sprintf("odù %q: expected edge %s not reproduced", label, key)
		}
	}
	for key := range actualSet {
		if _, ok := expectedSet[key]; !ok {
			return fmt.Sprintf("odù %q: derived unexpected edge %s not in the expected-edge-set", label, key)
		}
	}
	return ""
}
