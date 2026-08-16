// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

// TestDocumentationFamilyIsCatalogedAndResolvable pins the production
// coverage seam, not just the extractor helper used by the tests in
// materialized_edges_documentation_test.go. A manifest row cannot honestly
// resolve unless the installed binary carries the Odù and the
// materialized-edge resolver dispatches through the family's own exact guard
// -- and, distinctly for #5994's review finding, unless the compiled catalog
// Odù is the SAME facts the committed cassette drives live. Mirrors
// TestCodeCallFamilyIsCatalogedAndResolvable exactly.
func TestDocumentationFamilyIsCatalogedAndResolvable(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	catalog := CatalogByName()
	compiled, ok := catalog[documentationFamilyOduName]
	if !ok {
		t.Fatalf("CatalogByName omits %q", documentationFamilyOduName)
	}
	fromCassette, err := loadDocumentationFamilyOdu(documentationFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("loadDocumentationFamilyOdu: %v", err)
	}
	if !reflect.DeepEqual(compiled, fromCassette) {
		t.Fatalf("compiled catalog Odù drifted from strict cassette projection\ncompiled: %#v\ncassette: %#v", compiled, fromCassette)
	}

	ok, detail := (MaterializedEdgeOduResolver{Catalog: catalog, RepoRoot: repoRoot}).Resolve(replaycoverage.CoverageEntry{
		Surface:      MaterializedEdgeSurfacePrefix + "documentation_edges",
		Scenario:     replaycoverage.ScenarioOdu,
		ScenarioType: replaycoverage.ScenarioTypeBaseline,
		Ref:          documentationFamilyOduName,
	})
	if !ok {
		t.Fatalf("documentation_edges resolver rejected the cataloged Odù: %s", detail)
	}
	if !strings.Contains(detail, "expected 3-edge set exactly") {
		t.Fatalf("resolver detail = %q, want it to name the exact edge count it proved", detail)
	}
}

// TestDocumentationFamilyCassetteDerivesTheExpectedEdgeSet is the offline
// vacuity guard for #5994: the production extractor, over the committed
// cassette, reproduces the hand-derived expected set EXACTLY. Mirrors
// TestCodeCallFamilyCassetteDerivesTheExpectedEdgeSet.
//
// This is deliberately not called coverage by itself. It proves the
// extractor, not the gate: the live edge write is a MATCH-MERGE on endpoint
// uid, so a missing endpoint node would make the write a silent no-op this
// test cannot see. The live ifa-determinism/ifa-fault-injection assertions
// close that half and are what justify the committed coverage rows.
func TestDocumentationFamilyCassetteDerivesTheExpectedEdgeSet(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	odu, err := loadDocumentationFamilyOdu(documentationFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("loadDocumentationFamilyOdu: %v", err)
	}

	ok, detail := resolveDocumentationEdgeMaterializedEdges(odu, documentationFamilyExpectedEdgesPath(repoRoot))
	if !ok {
		t.Fatalf("documentation family cassette does not resolve: %s", detail)
	}
	t.Logf("%s", detail)
}

// TestDocumentationEdgesFamilyResolvesLiveEdgeTypes proves
// MaterializedEdgeDomainEdgeTypes resolves "documentation_edges" through the
// SingleTypeMaterializedEdgeTypes fallback (materialized_edge_families.go:84
// registers it), which is what backs `eshu-ifa assert-edges -domain
// documentation_edges` actually knowing which live graph edges belong to this
// family.
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

// TestLoadDocumentationFamilyOduRejectsUnknownJSONField mirrors
// TestLoadCodeCallFamilyOduRejectsUnknownJSONField: the cassette decoder
// catches a typo (e.g. "fact_knd" instead of "fact_kind") rather than
// silently decoding it away -- the same false-attestation shape #5994 forced
// a coverage-row withdrawal for. A well-formed cassette carrying every real
// envelope field still loads.
func TestLoadDocumentationFamilyOduRejectsUnknownJSONField(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	goodRaw := `{
  "collector": "git",
  "schema_version": "1",
  "scopes": [
    {
      "scope_id": "scope-1",
      "source_system": "git",
      "scope_kind": "repo",
      "collector_kind": "git",
      "partition_key": "scope-1",
      "metadata": {"fixture": "typo-probe"},
      "generation_id": "gen-1",
      "observed_at": "2026-08-09T00:00:00Z",
      "trigger_kind": "snapshot",
      "facts": [
        {
          "fact_kind": "content_entity",
          "schema_version": "1",
          "stable_fact_key": "k1",
          "collector_kind": "git",
          "source_confidence": "high",
          "payload": {"entity_id": "e1"}
        }
      ]
    }
  ]
}`
	goodPath := filepath.Join(dir, "good.json")
	if err := os.WriteFile(goodPath, []byte(goodRaw), 0o600); err != nil {
		t.Fatalf("write temp cassette: %v", err)
	}
	if _, err := loadDocumentationFamilyOdu(goodPath); err != nil {
		t.Fatalf("loadDocumentationFamilyOdu rejected a well-formed cassette carrying every real envelope field: %v", err)
	}

	badRaw := strings.Replace(goodRaw, `"fact_kind": "content_entity"`, `"fact_knd": "content_entity"`, 1)
	badPath := filepath.Join(dir, "typo.json")
	if err := os.WriteFile(badPath, []byte(badRaw), 0o600); err != nil {
		t.Fatalf("write temp cassette: %v", err)
	}
	if _, err := loadDocumentationFamilyOdu(badPath); err == nil {
		t.Fatal("loadDocumentationFamilyOdu accepted a cassette with an unknown field (fact_knd typo)")
	}
}
