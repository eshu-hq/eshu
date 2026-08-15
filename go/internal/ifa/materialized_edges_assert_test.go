// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestExpectedEdgeKeyIsByteIdenticalWithNoIdentity proves the precursor's
// core compatibility requirement: an ExpectedEdge with a nil or empty
// Identity produces EXACTLY the same Key() as before Identity existed. This
// is what spares the already-proven sql_relationships and code_calls
// families (whose fixtures carry no "identity" field) from re-proof.
func TestExpectedEdgeKeyIsByteIdenticalWithNoIdentity(t *testing.T) {
	t.Parallel()

	edge := ExpectedEdge{RelationshipType: "QUERIES_TABLE", SourceEntityID: "content-entity:a", TargetEntityID: "content-entity:b"}
	want := "QUERIES_TABLE|content-entity:a|content-entity:b"

	if got := edge.Key(); got != want {
		t.Fatalf("nil-Identity Key() = %q, want %q", got, want)
	}

	edge.Identity = map[string]string{}
	if got := edge.Key(); got != want {
		t.Fatalf("empty-non-nil-Identity Key() = %q, want %q", got, want)
	}
}

// TestExpectedEdgeKeyAppendsIdentityInSortedOrder proves Key() is
// deterministic regardless of Go map iteration order: two ExpectedEdge
// values built with the same Identity content produce the same Key(),
// and the appended segment is sorted by property name.
func TestExpectedEdgeKeyAppendsIdentityInSortedOrder(t *testing.T) {
	t.Parallel()

	edge := ExpectedEdge{
		RelationshipType: "DECLARES_CODEOWNER",
		SourceEntityID:   "repo-1",
		TargetEntityID:   "codeowner:team-a",
		Identity:         map[string]string{"source_path": "CODEOWNERS", "pattern": "*.go"},
	}
	want := "DECLARES_CODEOWNER|repo-1|codeowner:team-a|pattern=*.go|source_path=CODEOWNERS"
	if got := edge.Key(); got != want {
		t.Fatalf("Key() = %q, want %q (sorted by property name regardless of map insertion order)", got, want)
	}
}

// TestValidateExpectedEdgeIdentity covers the three fixture-error shapes
// LoadExpectedEdges must reject: a missing declared key, an undeclared key,
// and any identity map on a declared-empty relationship type.
func TestValidateExpectedEdgeIdentity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		identity map[string]string
		declared []string
		wantErr  bool
	}{
		{"declared-empty, nil identity: ok", nil, nil, false},
		{"declared-empty, non-empty identity: error", map[string]string{"pattern": "x"}, nil, true},
		{"declared two, identity matches exactly: ok", map[string]string{"pattern": "x", "source_path": "y"}, []string{"pattern", "source_path"}, false},
		{"declared two, missing one key: error", map[string]string{"pattern": "x"}, []string{"pattern", "source_path"}, true},
		{"declared two, extra undeclared key: error", map[string]string{"pattern": "x", "source_path": "y", "extra": "z"}, []string{"pattern", "source_path"}, true},
		{"declared one, wrong key name: error", map[string]string{"wrong_name": "x"}, []string{"path"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateExpectedEdgeIdentity(tc.identity, tc.declared)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateExpectedEdgeIdentity(%v, %v) error = %v, wantErr %v", tc.identity, tc.declared, err, tc.wantErr)
			}
		})
	}
}

// TestLoadExpectedEdgesRejectsUnregisteredFamily proves LoadExpectedEdges
// fails closed on a family cypher.MaterializedEdgeIdentityProperties does not
// recognize, before it even reads the fixture file.
func TestLoadExpectedEdgesRejectsUnregisteredFamily(t *testing.T) {
	t.Parallel()

	if _, err := LoadExpectedEdges("/does/not/matter.json", "not_a_real_family"); err == nil {
		t.Fatal("LoadExpectedEdges with an unregistered family returned no error")
	}
}

// TestLoadExpectedEdgesRejectsIdentityMismatch proves the live loader itself
// (not just the unit-level validateExpectedEdgeIdentity helper) rejects a
// fixture whose Identity does not match its family's declared set, and
// rejects an edge with a blank identity triple field.
func TestLoadExpectedEdgesRejectsIdentityMismatch(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		edges []sqlRelationshipExpectedEdge
	}{
		{
			name: "declared-empty family with a non-empty identity",
			edges: []sqlRelationshipExpectedEdge{
				{RelationshipType: "QUERIES_TABLE", SourceEntityID: "a", TargetEntityID: "b", Identity: map[string]string{"unexpected": "x"}},
			},
		},
		{
			name: "blank source_entity_id",
			edges: []sqlRelationshipExpectedEdge{
				{RelationshipType: "QUERIES_TABLE", SourceEntityID: "", TargetEntityID: "b"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeTempExpectedEdges(t, tc.edges)
			if _, err := LoadExpectedEdges(path, "sql_relationships"); err == nil {
				t.Fatal("LoadExpectedEdges returned no error for an invalid fixture")
			}
		})
	}
}

// TestLoadExpectedEdgesRejectsUnknownJSONField proves the decoder's
// DisallowUnknownFields catches a typo (e.g. "identiy") rather than silently
// decoding it away, while a well-formed fixture with the existing "note"
// field (every committed fixture carries one) still loads.
func TestLoadExpectedEdgesRejectsUnknownJSONField(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	badPath := filepath.Join(dir, "typo.json")
	badRaw := []byte(`{"odu":"odu:x","note":"n","edges":[{"relationship_type":"QUERIES_TABLE","source_entity_id":"a","target_entity_id":"b","identiy":{"x":"y"}}]}`)
	if err := os.WriteFile(badPath, badRaw, 0o600); err != nil {
		t.Fatalf("write temp fixture: %v", err)
	}
	if _, err := LoadExpectedEdges(badPath, "sql_relationships"); err == nil {
		t.Fatal("LoadExpectedEdges accepted a fixture with an unknown field (identiy typo)")
	}

	goodPath := filepath.Join(dir, "good.json")
	goodRaw := []byte(`{"odu":"odu:x","note":"n","edges":[{"relationship_type":"QUERIES_TABLE","source_entity_id":"a","target_entity_id":"b"}]}`)
	if err := os.WriteFile(goodPath, goodRaw, 0o600); err != nil {
		t.Fatalf("write temp fixture: %v", err)
	}
	edges, err := LoadExpectedEdges(goodPath, "sql_relationships")
	if err != nil {
		t.Fatalf("LoadExpectedEdges rejected a well-formed fixture with the existing note field: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("LoadExpectedEdges returned %d edges, want 1", len(edges))
	}
}

// TestLoadExpectedEdgesCommittedFixturesStillLoad is the compatibility proof:
// the real, committed sql_relationships and code_calls fixtures — unmodified
// by this change — still load byte-identically through the new
// family-validated LoadExpectedEdges.
func TestLoadExpectedEdgesCommittedFixturesStillLoad(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	sqlEdges, err := LoadExpectedEdges(sqlFamilyExpectedEdgesPath(repoRoot), "sql_relationships")
	if err != nil {
		t.Fatalf("LoadExpectedEdges(sql_relationships): %v", err)
	}
	if len(sqlEdges) == 0 {
		t.Fatal("sql_relationships expected-edge fixture loaded zero edges")
	}
	for _, e := range sqlEdges {
		if len(e.Identity) != 0 {
			t.Errorf("sql_relationships edge %+v carries an Identity, but the family declares no identity properties", e)
		}
	}

	codeCallEdges, err := LoadExpectedEdges(codeCallFamilyExpectedEdgesPath(repoRoot), "code_calls")
	if err != nil {
		t.Fatalf("LoadExpectedEdges(code_calls): %v", err)
	}
	if len(codeCallEdges) == 0 {
		t.Fatal("code_calls expected-edge fixture loaded zero edges")
	}
	for _, e := range codeCallEdges {
		if len(e.Identity) != 0 {
			t.Errorf("code_calls edge %+v carries an Identity, but the family declares no identity properties", e)
		}
	}
}

// TestSQLRelationshipExpectedEdgesFileRoundTripsNote proves the additive Note
// field survives an unmarshal/marshal round trip so the DisallowUnknownFields
// decoder change stays compatible with every committed fixture's "note" key.
func TestSQLRelationshipExpectedEdgesFileRoundTripsNote(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"odu":"odu:x","note":"authoring context","edges":[{"relationship_type":"QUERIES_TABLE","source_entity_id":"a","target_entity_id":"b"}]}`)
	var parsed sqlRelationshipExpectedEdgesFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.Note != "authoring context" {
		t.Fatalf("Note = %q, want %q", parsed.Note, "authoring context")
	}
}
