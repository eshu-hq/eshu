// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// documentationLiveCassettePath and documentationLiveExpectedEdgesPath are the
// live-gate fixtures for the documentation_edges family (#5994), distinct from
// the offline pure-guard fixtures (documentationFamilyOduName,
// documentationFamilyExpectedEdgesPath) that materialized_edges_documentation.go
// and documentation_family_odu.go already prove. The offline pair proves the
// EXTRACTOR against a hand-authored, table-kind-including Odù with no graph
// backend; this pair proves the same extractor's output against a checked-in
// cassette that `eshu-ifa drive` replays into a real Postgres+graph stack and
// `eshu-ifa assert-edges -domain documentation_edges` checks live. They are
// deliberately independent artifacts (nothing enforces they describe the same
// scenario) — this test is the one thing that keeps the cassette and its live
// expected-edge fixture from drifting apart silently.
//
// Derivation record for the live expected-edges fixture (kept here rather
// than as a JSON field: sqlRelationshipExpectedEdgesFile, the shared struct
// LoadExpectedEdges/loadSQLRelationshipExpectedEdges decodes into, declares
// only "odu" and "edges" — encoding/json currently accepts an extra field
// silently, but that laxness is not a contract to build fixtures against).
// It deliberately carries only entity-kind (Function/Class) targets, not a
// SqlTable target: batchCanonicalDocumentationEntityEdgeCypher's MATCH label
// alternation (Function|Class|Struct|Interface|TypeAlias|Enum|File) excludes
// SqlTable, so a table-kind mention's DOCUMENTS edge cannot materialize
// against a live backend today (go/internal/storage/cypher/
// canonical_documentation_edges.go, pinned by
// TestBuildDocumentationRowMapTableTargetMatchesSqlTableLabel). Each edge's
// source_entity_id is the DocumentationSection node's uid,
// documentationSectionNodeUID(document_id, section_id) = "docsection:" +
// document_id + "|" + section_id (documentation_edge_materialization.go);
// DocumentationSection is not in projector.canonicalNamePathLineEntityLabels
// so its uid is exactly the row's own section_uid, not a recomputed hash.
// Each edge's target_entity_id is content.CanonicalEntityID(repo_id,
// relative_path, entity_type, entity_name, start_line)
// (go/internal/content/writer.go:196) because Function and Class ARE
// canonicalNamePathLineEntityLabels (go/internal/projector/
// canonical_entity_identity.go:12): the projector ignores the incoming
// content_entity entity_id for these labels and derives the uid hash, so the
// cassette precomputes the same value in content_entity.entity_id AND the
// mention's candidate_refs[].id (the code_calls-family precedent,
// go/internal/ifa/code_call_family_catalog.go). The fixture's top-level "odu"
// field is NOT validated against Catalog()/CatalogByName() or anything else
// -- loadSQLRelationshipExpectedEdges decodes it and never reads it back —
// so "cassette:ifa-documentation-family" here is a human label, not a
// resolvable identity.
const (
	documentationLiveCassettePath      = "testdata/cassettes/documentation/ifa-documentation-family.json"
	documentationLiveExpectedEdgesPath = "go/internal/ifa/testdata/documentation/ifa-documentation-family-live-expected-edges.json"
)

// documentationLiveCassetteFact and documentationLiveCassetteFile mirror the
// cassette envelope fields loadCodeCallFamilyOdu (code_call_family_odu.go)
// reads: fact_kind, stable_fact_key, schema_version, collector_kind,
// source_confidence, and payload. A hand-rolled minimal struct, not the full
// replay/cassette.File type, because this test only needs to reproduce the
// same facts.Envelope slice the real replay driver builds, not its scope
// metadata (observed_at, trigger_kind, ...).
type documentationLiveCassetteFact struct {
	FactKind         string         `json:"fact_kind"`
	StableFactKey    string         `json:"stable_fact_key"`
	SchemaVersion    string         `json:"schema_version"`
	CollectorKind    string         `json:"collector_kind"`
	SourceConfidence string         `json:"source_confidence"`
	Payload          map[string]any `json:"payload"`
}

type documentationLiveCassetteScope struct {
	ScopeID      string                          `json:"scope_id"`
	GenerationID string                          `json:"generation_id"`
	Facts        []documentationLiveCassetteFact `json:"facts"`
}

type documentationLiveCassetteFile struct {
	Scopes []documentationLiveCassetteScope `json:"scopes"`
}

// loadDocumentationLiveCassetteEnvelopes reads the committed live-gate
// cassette and projects it onto the fact envelopes the reducer's extractor
// consumes, the same projection loadCodeCallFamilyOdu performs for the
// code_calls family. It fails closed on a missing file, a non-single-scope
// cassette, or an empty fact list — a silently empty cassette would make the
// exact-set comparison below vacuously pass.
func loadDocumentationLiveCassetteEnvelopes(cassettePath string) (string, []facts.Envelope, error) {
	raw, err := os.ReadFile(cassettePath) // #nosec G304 -- checked-in repo fixture under testdata/, not external input
	if err != nil {
		return "", nil, err
	}
	var parsed documentationLiveCassetteFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", nil, err
	}
	if len(parsed.Scopes) != 1 {
		return "", nil, fmt.Errorf("ifa: documentation live cassette %s declares %d scope(s), want exactly 1; a multi-scope fixture would make the expected-edge set ambiguous about which scope produced an edge", cassettePath, len(parsed.Scopes))
	}
	scope := parsed.Scopes[0]
	if len(scope.Facts) == 0 {
		return "", nil, fmt.Errorf("ifa: documentation live cassette %s carries no facts; an empty cassette makes every assertion vacuous", cassettePath)
	}
	envelopes := make([]facts.Envelope, 0, len(scope.Facts))
	for _, f := range scope.Facts {
		envelopes = append(envelopes, facts.Envelope{
			ScopeID:          scope.ScopeID,
			GenerationID:     scope.GenerationID,
			FactKind:         f.FactKind,
			SchemaVersion:    f.SchemaVersion,
			StableFactKey:    f.StableFactKey,
			CollectorKind:    f.CollectorKind,
			SourceConfidence: f.SourceConfidence,
			Payload:          f.Payload,
		})
	}
	return scope.ScopeID, envelopes, nil
}

// TestDocumentationLiveCassetteMatchesLiveExpectedEdges proves the checked-in
// live cassette and its expected-edge fixture describe the SAME scenario: the
// pure extractor run over the cassette's facts reproduces the live fixture's
// exact DOCUMENTS edge set. This is the hermetic half of the live-gate proof
// (no Postgres or graph backend); `eshu-ifa drive` + `eshu-ifa assert-edges
// -domain documentation_edges` is the live half that also proves the writer's
// MATCH/MERGE actually finds the endpoint nodes this test cannot check.
func TestDocumentationLiveCassetteMatchesLiveExpectedEdges(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	scopeID, envelopes, err := loadDocumentationLiveCassetteEnvelopes(filepath.Join(repoRoot, documentationLiveCassettePath))
	if err != nil {
		t.Fatalf("load documentation live cassette: %v", err)
	}

	rows, quarantined, err := reducer.ExtractDocumentationEdgeRowsWithQuarantine(envelopes, scopeID)
	if err != nil {
		t.Fatalf("ExtractDocumentationEdgeRowsWithQuarantine: %v", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %d, want 0: %#v", len(quarantined), quarantined)
	}

	expected, err := LoadExpectedEdges(filepath.Join(repoRoot, documentationLiveExpectedEdgesPath))
	if err != nil {
		t.Fatalf("LoadExpectedEdges: %v", err)
	}

	got := make([]string, 0, len(rows))
	for _, row := range rows {
		got = append(got, "DOCUMENTS|"+anyToStringValue(row["section_uid"])+"|"+anyToStringValue(row["target_entity_id"]))
	}
	want := make([]string, 0, len(expected))
	for _, e := range expected {
		want = append(want, e.Key())
	}
	sort.Strings(got)
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("cassette extraction produced %d edge(s), live fixture expects %d: got=%v want=%v", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("cassette/live-fixture drift at index %d: got %q, want %q (full got=%v want=%v)", i, got[i], want[i], got, want)
		}
	}
}

// TestDocumentationEdgesFamilyResolvesLiveEdgeTypes proves
// MaterializedEdgeDomainEdgeTypes resolves "documentation_edges" through the
// SingleTypeMaterializedEdgeTypes fallback (materialized_edge_families.go:84
// registers it), which is what backs `eshu-ifa assert-edges -domain
// documentation_edges` actually knowing which live graph edges belong to this
// family. specs/ifa-materialized-edge-coverage.v1.yaml's waiver comment
// currently claims this rejects outright; it does not, and that comment needs
// correcting in the same change that retires the waivers.
func TestDocumentationEdgesFamilyResolvesLiveEdgeTypes(t *testing.T) {
	t.Parallel()
	types, err := MaterializedEdgeDomainEdgeTypes("documentation_edges")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(documentation_edges) = error %v, want it to resolve", err)
	}
	if _, ok := types["DOCUMENTS"]; !ok || len(types) != 1 {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(documentation_edges) = %v, want exactly {DOCUMENTS}", types)
	}
}
