// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

// TestCodeCallFamilyCassetteDerivesTheExpectedEdgeSet is the offline vacuity
// guard for #5991: the production extractor, over the committed cassette,
// reproduces the hand-derived expected set EXACTLY.
//
// Exact in both directions. MISSING means the extractor stopped producing an
// edge the graph is supposed to carry; EXTRA means it started producing one
// nobody derived — the direction a "contains all expected" check misses, and the
// one that silently grows the graph.
//
// This is deliberately NOT called coverage. It proves the extractor, not the
// gate: the live edge write is a MATCH-MATCH-MERGE on endpoint uid, so a missing
// endpoint node makes the write a silent no-op that this test cannot see. The
// live ifa-determinism assertion closes that half and is what justifies the
// committed baseline coverage row.
func TestCodeCallFamilyCassetteDerivesTheExpectedEdgeSet(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	odu, err := loadCodeCallFamilyOdu(codeCallFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("loadCodeCallFamilyOdu: %v", err)
	}

	expectedEdges, err := LoadExpectedEdges(codeCallFamilyExpectedEdgesPath(repoRoot), "code_calls")
	if err != nil {
		t.Fatalf("LoadExpectedEdges: %v", err)
	}
	if len(expectedEdges) == 0 {
		t.Fatal("expected-edge set is empty; an empty expectation makes this guard vacuous")
	}

	_, codeCallRows, _, metaclassRows := reducer.ExtractAllCodeRelationshipRows(odu.Facts)

	actual := map[string]int{}
	for _, row := range codeCallRows {
		actual[codeCallEdgeKey(
			anyToStringValue(row["relationship_type"]),
			anyToStringValue(row["caller_entity_id"]),
			anyToStringValue(row["callee_entity_id"]),
		)]++
	}
	for _, row := range metaclassRows {
		// Metaclass rows carry source/target rather than caller/callee.
		actual[codeCallEdgeKey(
			anyToStringValue(row["relationship_type"]),
			anyToStringValue(row["source_entity_id"]),
			anyToStringValue(row["target_entity_id"]),
		)]++
	}

	expected := map[string]int{}
	for _, e := range expectedEdges {
		expected[codeCallEdgeKey(e.RelationshipType, e.SourceEntityID, e.TargetEntityID)]++
	}

	var missing, extra []string
	for key, want := range expected {
		if actual[key] < want {
			missing = append(missing, key)
		}
	}
	for key, got := range actual {
		if got > expected[key] {
			extra = append(extra, key)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	if len(missing) > 0 {
		t.Errorf("MISSING %d edge(s) the extractor no longer produces: %s", len(missing), strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		t.Errorf("EXTRA %d edge(s) nobody derived: %s", len(extra), strings.Join(extra, ", "))
	}
	if len(missing) == 0 && len(extra) == 0 {
		t.Logf("code_calls: %d rows reproduce the expected %d-edge set exactly", len(codeCallRows)+len(metaclassRows), len(expectedEdges))
	}
}

// TestCodeCallFamilyCoversAllFourEdgeTypes stops the fixture degrading into a
// one-type proof.
//
// The family owns four types and the whole point of #5991 is exhaustiveness. An
// expected set that quietly lost INSTANTIATES or USES_METACLASS would still pass
// the exact-set test above — it would simply be exact about less.
func TestCodeCallFamilyCoversAllFourEdgeTypes(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	expectedEdges, err := LoadExpectedEdges(codeCallFamilyExpectedEdgesPath(repoRoot), "code_calls")
	if err != nil {
		t.Fatalf("LoadExpectedEdges: %v", err)
	}

	present := map[string]struct{}{}
	for _, e := range expectedEdges {
		present[e.RelationshipType] = struct{}{}
	}
	registered, err := MaterializedEdgeDomainEdgeTypes("code_calls")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(code_calls): %v", err)
	}
	var uncovered []string
	for edgeType := range registered {
		if _, ok := present[edgeType]; !ok {
			uncovered = append(uncovered, edgeType)
		}
	}
	sort.Strings(uncovered)
	if len(uncovered) > 0 {
		t.Errorf("the expected-edge set exercises no %v edge; the family registers them, so the fixture proves exhaustiveness over less than the family owns", uncovered)
	}
}

// TestCodeCallFamilyIsCatalogedAndResolvable pins the production coverage
// seam, not just the extractor helper used by the tests below. A manifest row
// cannot honestly resolve unless the installed binary carries the Odù and the
// materialized-edge resolver dispatches through the family's own exact guard.
func TestCodeCallFamilyIsCatalogedAndResolvable(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	catalog := CatalogByName()
	compiled, ok := catalog[codeCallFamilyOduName]
	if !ok {
		t.Fatalf("CatalogByName omits %q", codeCallFamilyOduName)
	}
	fromCassette, err := loadCodeCallFamilyOdu(codeCallFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("loadCodeCallFamilyOdu: %v", err)
	}
	if !reflect.DeepEqual(compiled, fromCassette) {
		t.Fatalf("compiled catalog Odù drifted from strict cassette projection\ncompiled: %#v\ncassette: %#v", compiled, fromCassette)
	}

	ok, detail := (MaterializedEdgeOduResolver{Catalog: catalog, RepoRoot: repoRoot}).Resolve(replaycoverage.CoverageEntry{
		Surface:      MaterializedEdgeSurfacePrefix + "code_calls",
		Scenario:     replaycoverage.ScenarioOdu,
		ScenarioType: replaycoverage.ScenarioTypeBaseline,
		Ref:          codeCallFamilyOduName,
	})
	if !ok {
		t.Fatalf("code_calls resolver rejected the cataloged Odù: %s", detail)
	}
	if !strings.Contains(detail, "expected 5-edge set exactly") || !strings.Contains(detail, "all 4 registry types") {
		t.Fatalf("resolver detail = %q, want exact edge and registry-type counts", detail)
	}
}

// TestCodeCallFamilyRepositoryIdentityDoesNotCollideWithSQLFamily pins the
// repository and generation identity boundaries exercised by the live
// determinism matrix. A shared local_path lets canonical path cleanup delete
// the other repository; a shared generation ID violates the durable active
// generation uniqueness constraint when both scopes ACK.
func TestCodeCallFamilyRepositoryIdentityDoesNotCollideWithSQLFamily(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu, err := loadCodeCallFamilyOdu(codeCallFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("loadCodeCallFamilyOdu: %v", err)
	}

	localPath := ""
	for _, fact := range odu.Facts {
		if fact.FactKind != "repository" {
			continue
		}
		localPath = strings.TrimSpace(anyToStringValue(fact.Payload["local_path"]))
		break
	}
	if localPath == "" {
		t.Fatal("code-call repository has no local_path; canonical file and entity paths would be unanchored")
	}
	if localPath == sqlFamilyLocalPath {
		t.Fatalf("code-call local_path %q collides with SQL-family repository path; canonical path cleanup can delete the other live-matrix repository", localPath)
	}
	if len(odu.Facts) == 0 || strings.TrimSpace(odu.Facts[0].GenerationID) == "" {
		t.Fatal("code-call Odù has no generation ID; active-generation publication would be unidentifiable")
	}
	if odu.Facts[0].GenerationID == sqlFamilyGenerationID {
		t.Fatalf("code-call generation ID %q collides with SQL-family generation; only one live-matrix scope can publish it as active", odu.Facts[0].GenerationID)
	}
}

// TestCodeCallOduPreservesEnvelopeFields stops the loader being more permissive
// than production.
//
// schema_version was originally dropped by this projection. An empty version
// reads as "latest", so a cassette carrying an unsupported major would satisfy
// this guard while live replay preserved the version and quarantined the fact —
// the offline proof would certify input the live gate rejects, which is the one
// failure this fixture exists to make impossible.
func TestCodeCallOduPreservesEnvelopeFields(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	odu, err := loadCodeCallFamilyOdu(codeCallFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("loadCodeCallFamilyOdu: %v", err)
	}

	// Read the cassette independently so the comparison is against the file,
	// not against the loader's own view of it.
	raw, err := os.ReadFile(codeCallFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("read cassette: %v", err)
	}
	var onDisk struct {
		Scopes []struct {
			Facts []struct {
				FactKind      string `json:"fact_kind"`
				SchemaVersion string `json:"schema_version"`
				StableFactKey string `json:"stable_fact_key"`
				CollectorKind string `json:"collector_kind"`
			} `json:"facts"`
		} `json:"scopes"`
	}
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("parse cassette: %v", err)
	}
	if len(onDisk.Scopes) != 1 || len(onDisk.Scopes[0].Facts) != len(odu.Facts) {
		t.Fatalf("fact count mismatch: cassette has %d, Odù has %d", len(onDisk.Scopes[0].Facts), len(odu.Facts))
	}

	checked := 0
	for i, want := range onDisk.Scopes[0].Facts {
		got := odu.Facts[i]
		if want.SchemaVersion == "" {
			t.Fatalf("fact %d (%s) declares no schema_version; this guard would be vacuous", i, want.FactKind)
		}
		if got.SchemaVersion != want.SchemaVersion {
			t.Errorf("fact %d (%s): schema_version %q dropped or altered (cassette says %q); an empty version reads as latest and hides an unsupported major",
				i, want.FactKind, got.SchemaVersion, want.SchemaVersion)
		}
		if got.StableFactKey != want.StableFactKey {
			t.Errorf("fact %d (%s): stable_fact_key %q != cassette %q", i, want.FactKind, got.StableFactKey, want.StableFactKey)
		}
		if got.CollectorKind != want.CollectorKind {
			t.Errorf("fact %d (%s): collector_kind %q != cassette %q", i, want.FactKind, got.CollectorKind, want.CollectorKind)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no facts compared; this guard would pass vacuously")
	}
	t.Logf("verified envelope fields preserved across %d facts", checked)
}

// TestLoadCodeCallFamilyOduRejectsUnknownJSONField proves the cassette
// decoder catches a typo (e.g. "fact_knd" instead of "fact_kind") rather than
// silently decoding it away -- the same false-attestation shape #5994 forced
// a coverage-row withdrawal for: an unnoticed typo here would silently
// project a WRONG fact set from a cassette, and the resulting Odù would still
// drive an exact-set gate, whose green result would then attest to something
// the cassette did not actually say. A well-formed cassette carrying every
// real envelope field (collector, schema_version, source_system, scope_kind,
// partition_key, metadata, observed_at, trigger_kind) -- the full shape the
// real committed cassette carries, not just the load-bearing subset this
// loader consumes -- still loads.
func TestLoadCodeCallFamilyOduRejectsUnknownJSONField(t *testing.T) {
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
	if _, err := loadCodeCallFamilyOdu(goodPath); err != nil {
		t.Fatalf("loadCodeCallFamilyOdu rejected a well-formed cassette carrying every real envelope field: %v", err)
	}

	badRaw := strings.Replace(goodRaw, `"fact_kind": "content_entity"`, `"fact_knd": "content_entity"`, 1)
	badPath := filepath.Join(dir, "typo.json")
	if err := os.WriteFile(badPath, []byte(badRaw), 0o600); err != nil {
		t.Fatalf("write temp cassette: %v", err)
	}
	if _, err := loadCodeCallFamilyOdu(badPath); err == nil {
		t.Fatal("loadCodeCallFamilyOdu accepted a cassette with an unknown field (fact_knd typo)")
	}
}
