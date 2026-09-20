// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

// TestRationaleFamilyOduResolvesItsExpectedEdgeSet proves the EXTRACTOR:
// ExtractRationaleEdgeRows, over the cataloged Odù, reproduces the hand-derived
// edge set exactly.
//
// Deliberately not called coverage.
// MaterializedEdgeDomainEdgeTypes recognizes rationale_edges as EXPLAINS.
// Both live gates drive the rationale cassette and exact-assert its full EXPLAINS records.
// This test remains the extractor half of that proof: it cannot catch a live
// writer break by itself, while the manifest's live rows bind the same fixture
// to the backend assertions.
func TestRationaleFamilyOduResolvesItsExpectedEdgeSet(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu := ifa.CatalogByName()[ifa.RationaleFamilyOduName]
	if odu.Name == "" {
		t.Fatalf("Odù %q is not in the catalog", ifa.RationaleFamilyOduName)
	}

	ok, detail := resolveRationaleEdgeMaterializedEdges(odu, rationaleFamilyExpectedEdgesPath(repoRoot))
	if !ok {
		t.Fatalf("rationale family Odù does not resolve: %s", detail)
	}
	if !strings.Contains(detail, "expected 3-edge set exactly") {
		t.Errorf("detail = %q, want it to name the exact edge count it proved", detail)
	}
	t.Logf("%s", detail)
}

// TestRationaleExpectedSetFeedsLiveAssertEdges links the extractor-validated
// rationale fixture to the complete records consumed by `ifa assert-edges`'s
// rationale branch. The offline guard alone cannot prove the live loader sees
// an EXPLAINS type or its Rationale source uid.
func TestRationaleExpectedSetFeedsLiveAssertEdges(t *testing.T) {
	t.Parallel()
	path := rationaleFamilyExpectedEdgesPath(repoRootDir(t))
	rationaleEdges, err := loadRationaleExpectedEdges(path)
	if err != nil {
		t.Fatalf("loadRationaleExpectedEdges: %v", err)
	}
	_, liveEdges, err := LoadRationaleExpectedEdgeRecords(path)
	if err != nil {
		t.Fatalf("LoadRationaleExpectedEdgeRecords: %v", err)
	}
	if len(liveEdges) != len(rationaleEdges) {
		t.Fatalf("LoadRationaleExpectedEdgeRecords returned %d edges, want %d extractor-validated rationale edges", len(liveEdges), len(rationaleEdges))
	}
	for i, rationaleEdge := range rationaleEdges {
		if rationaleEdge.RelationshipType != "EXPLAINS" || rationaleEdge.SourceEntityID != rationaleEdge.RationaleUID {
			t.Fatalf("rationale edge %d live identity = %#v, want EXPLAINS sourced from rationale uid %q", i, liveEdges[i], rationaleEdge.RationaleUID)
		}
		if !reflect.DeepEqual(liveEdges[i], rationaleEdge) {
			t.Errorf("live assertion edge %d = %#v, want %#v", i, liveEdges[i], rationaleEdge)
		}
	}
}

// TestRationaleCatalogMatchesReplayCassette keeps the compiled catalog Odù and
// live replay input structurally equal at the facts.Envelope boundary after
// normalizing transport fields that replay derives: FactID, fencing,
// observation time, and SourceRef.
func TestRationaleCatalogMatchesReplayCassette(t *testing.T) {
	t.Parallel()
	path := filepath.Join(repoRootDir(t), ifa.RationaleFamilyCassetteRelPath)
	source, err := cassette.NewSource(path)
	if err != nil {
		t.Fatalf("cassette.NewSource: %v", err)
	}
	generation, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("cassette source Next: %v", err)
	}
	if !ok {
		t.Fatal("cassette source returned no generation")
	}

	cassetteFacts := make([]facts.Envelope, 0, generation.FactCount())
	for envelope := range generation.Facts {
		cassetteFacts = append(cassetteFacts, envelope)
	}
	compiled := ifa.CatalogByName()[ifa.RationaleFamilyOduName]
	if compiled.Name == "" {
		t.Fatalf("CatalogByName omits %q", ifa.RationaleFamilyOduName)
	}
	if len(cassetteFacts) != len(compiled.Facts) {
		t.Fatalf("cassette has %d facts, compiled catalog Odù has %d", len(cassetteFacts), len(compiled.Facts))
	}
	for i := range cassetteFacts {
		got := cassetteFacts[i]
		got.FactID = ""
		got.FencingToken = 0
		got.ObservedAt = time.Time{}
		got.SourceRef = facts.Ref{}
		if !reflect.DeepEqual(got, compiled.Facts[i]) {
			t.Errorf("fact %d drifted between replay cassette and catalog\ncassette: %#v\ncatalog: %#v", i, got, compiled.Facts[i])
		}
	}
	if _, ok, err := source.Next(context.Background()); err != nil || ok {
		t.Fatalf("cassette declares more than one generation: ok=%v err=%v", ok, err)
	}
}

// TestRationaleFamilyResolvesThroughTheManifestResolver proves the family is
// reachable the way the gate reaches it — by surface name through
// MaterializedEdgeOduResolver. A guard nothing dispatches to is not coverage.
func TestRationaleFamilyResolvesThroughTheManifestResolver(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	resolver := MaterializedEdgeOduResolver{Catalog: ifa.CatalogByName(), RepoRoot: repoRoot}

	ok, detail := resolver.Resolve(replaycoverage.CoverageEntry{
		Surface:      MaterializedEdgeSurfacePrefix + "rationale_edges",
		Scenario:     replaycoverage.ScenarioOdu,
		ScenarioType: replaycoverage.ScenarioTypeBaseline,
		Ref:          ifa.RationaleFamilyOduName,
	})
	if !ok {
		t.Fatalf("resolver.Resolve for rationale_edges: %s", detail)
	}
}

// TestRationaleFamilyExpectedSetRejectsAnExtraEdge is the teeth test in the
// direction a "contains all expected" check would miss: an edge nobody derived
// must fail as loudly as a missing one, or the graph can grow silently.
func TestRationaleFamilyExpectedSetRejectsAnExtraEdge(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu := ifa.CatalogByName()[ifa.RationaleFamilyOduName]

	expected, err := loadRationaleExpectedEdges(rationaleFamilyExpectedEdgesPath(repoRoot))
	if err != nil {
		t.Fatalf("loadRationaleExpectedEdges: %v", err)
	}
	short := rationaleExpectedEdgesFile{Odu: ifa.RationaleFamilyOduName, Edges: expected[:len(expected)-1]}
	path := writeRationaleExpectedEdges(t, short)

	ok, detail := resolveRationaleEdgeMaterializedEdges(odu, path)
	if ok {
		t.Fatal("guard passed against a short expected set; an edge nobody derived went unreported")
	}
	if !strings.Contains(detail, "EXTRA") {
		t.Errorf("detail = %q, want it to name the EXTRA edge", detail)
	}
}

// TestRationaleFamilyExpectedSetRejectsAMissingEdge is the other direction: an
// expectation naming an edge the extractor does not produce must fail, or the
// fixture could quietly outrun the code.
func TestRationaleFamilyExpectedSetRejectsAMissingEdge(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu := ifa.CatalogByName()[ifa.RationaleFamilyOduName]

	expected, err := loadRationaleExpectedEdges(rationaleFamilyExpectedEdgesPath(repoRoot))
	if err != nil {
		t.Fatalf("loadRationaleExpectedEdges: %v", err)
	}
	padded := rationaleExpectedEdgesFile{
		Odu: ifa.RationaleFamilyOduName,
		Edges: append(append([]rationaleExpectedEdge{}, expected...), rationaleExpectedEdge{
			RationaleUID:   "rationale:func:payments.Refund:why:deadbeefdeadbeef",
			TargetEntityID: "func:payments.Refund",
			TargetPath:     "services/payments/refund.go",
			RepoID:         ifa.RationaleFamilyRepoID,
			CommentKind:    "why",
		}),
	}
	path := writeRationaleExpectedEdges(t, padded)

	ok, detail := resolveRationaleEdgeMaterializedEdges(odu, path)
	if ok {
		t.Fatal("guard passed against an expectation the extractor does not satisfy")
	}
	if !strings.Contains(detail, "MISSING") {
		t.Errorf("detail = %q, want it to name the MISSING edge", detail)
	}
}

// TestRationaleFamilyKindIsPartOfTheNodeIdentity pins the pair of edges that
// share an excerpt hash.
//
// Both come from the same comment text under different kinds. If the Rationale
// UID were built from entity + hash alone they would collapse to one edge, and
// every other assertion here would still pass with a graph missing an edge. The
// shared hash is what makes that collapse possible, so it is asserted directly
// rather than left implicit in the fixture.
func TestRationaleFamilyKindIsPartOfTheNodeIdentity(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	expected, err := loadRationaleExpectedEdges(rationaleFamilyExpectedEdgesPath(repoRoot))
	if err != nil {
		t.Fatalf("loadRationaleExpectedEdges: %v", err)
	}

	byHash := map[string][]rationaleExpectedEdge{}
	for _, e := range expected {
		parts := strings.Split(e.RationaleUID, ":")
		byHash[parts[len(parts)-1]] = append(byHash[parts[len(parts)-1]], e)
	}

	shared := 0
	for hash, edges := range byHash {
		if len(edges) < 2 {
			continue
		}
		shared++
		kinds := map[string]struct{}{}
		for _, e := range edges {
			kinds[e.CommentKind] = struct{}{}
		}
		if len(kinds) != len(edges) {
			t.Errorf("excerpt hash %s is shared by %d edges but only %d distinct kind(s); they would collapse if kind left the UID", hash, len(edges), len(kinds))
		}
	}
	if shared == 0 {
		t.Error("no two expected edges share an excerpt hash; the fixture no longer proves that comment kind is part of the Rationale node identity")
	}
}

// TestRationaleFamilyOduPinsParserReachableExclusions pins the live fixture's
// parser-reachable negative cases without injecting malformed fact envelopes.
func TestRationaleFamilyOduPinsParserReachableExclusions(t *testing.T) {
	t.Parallel()
	odu := ifa.CatalogByName()[ifa.RationaleFamilyOduName]
	if got, want := len(odu.Facts), 12; got != want {
		t.Fatalf("rationale Odù fact count = %d, want %d", got, want)
	}

	var contentEntities, duplicateComments, emptyTODO int
	pathsWithoutComments := map[string]bool{
		ifa.RationaleFamilyRefundPath:      false,
		ifa.RationaleFamilyReconcilePath:   false,
		ifa.RationaleFamilyHealthcheckPath: false,
	}
	seen := map[string]int{}
	for _, env := range odu.Facts {
		if env.FactKind != "content_entity" {
			continue
		}
		contentEntities++
		entityID := strings.TrimSpace(anyToStringValue(env.Payload["entity_id"]))
		if env.IsTombstone || entityID == "" || anyToStringValue(env.Payload["repo_id"]) != ifa.RationaleFamilyRepoID {
			t.Errorf("live content entity %q is malformed or tombstoned", env.StableFactKey)
		}
		comments := []map[string]any(nil)
		if meta, ok := env.Payload["entity_metadata"].(map[string]any); ok {
			comments = rationaleFixtureComments(meta["rationale_comments"])
		}
		path := anyToStringValue(env.Payload["relative_path"])
		if _, tracked := pathsWithoutComments[path]; tracked {
			pathsWithoutComments[path] = len(comments) == 0
		}
		for _, c := range comments {
			kind := strings.TrimSpace(anyToStringValue(c["kind"]))
			text := strings.TrimSpace(anyToStringValue(c["text"]))
			if kind == "TODO" && text == "" {
				emptyTODO++
				continue
			}
			key := entityID + "\x00" + kind + "\x00" + text
			seen[key]++
			if seen[key] > 1 {
				duplicateComments++
			}
		}
	}
	if contentEntities != 5 || duplicateComments != 1 || emptyTODO != 1 {
		t.Errorf("fixture guards = entities:%d duplicate:%d empty TODO:%d, want 5/1/1", contentEntities, duplicateComments, emptyTODO)
	}
	for path, noComments := range pathsWithoutComments {
		if !noComments {
			t.Errorf("parser-negative fixture %q unexpectedly carries rationale comments", path)
		}
	}
}

// TestLoadRationaleExpectedEdgesRejectsUnknownJSONField proves the decoder's
// DisallowUnknownFields catches a typo in a fixture (e.g. "target_pth"
// instead of "target_path") rather than silently decoding it away -- the
// same class of gap #5992 review found and fixed for the SQL family's
// loader (loadSQLRelationshipExpectedEdges) -- while a well-formed fixture
// with the existing "note" field (the real committed fixture carries one)
// still loads.
func TestLoadRationaleExpectedEdgesRejectsUnknownJSONField(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	badPath := filepath.Join(dir, "typo.json")
	badRaw := []byte(`{"odu":"odu:x","note":"n","edges":[{"rationale_uid":"r","target_entity_id":"t","target_pth":"p","repo_id":"repo","comment_kind":"why"}]}`)
	if err := os.WriteFile(badPath, badRaw, 0o600); err != nil {
		t.Fatalf("write temp fixture: %v", err)
	}
	if _, err := loadRationaleExpectedEdges(badPath); err == nil {
		t.Fatal("loadRationaleExpectedEdges accepted a fixture with an unknown field (target_pth typo)")
	}

	goodPath := filepath.Join(dir, "good.json")
	goodRaw := []byte(`{"odu":"odu:x","note":"n","edges":[{"rationale_uid":"r","target_entity_id":"t","target_path":"p","repo_id":"repo","comment_kind":"why"}]}`)
	if err := os.WriteFile(goodPath, goodRaw, 0o600); err != nil {
		t.Fatalf("write temp fixture: %v", err)
	}
	edges, err := loadRationaleExpectedEdges(goodPath)
	if err != nil {
		t.Fatalf("loadRationaleExpectedEdges rejected a well-formed fixture with the existing note field: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("loadRationaleExpectedEdges returned %d edges, want 1", len(edges))
	}
}

// TestLoadRationaleExpectedEdgesRejectsTrailingJSON mirrors
// TestLoadSQLRelationshipExpectedEdgesRejectsTrailingJSON: switching to
// json.Decoder for DisallowUnknownFields must not silently reopen the
// trailing-content gap json.Unmarshal (this loader's prior decoder) closed
// for free -- json.Decoder.Decode reads exactly one JSON value and stops.
func TestLoadRationaleExpectedEdgesRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	first := `{"odu":"odu:x","note":"n","edges":[{"rationale_uid":"r","target_entity_id":"t","target_path":"p","repo_id":"repo","comment_kind":"why"}]}`
	second := `{"odu":"odu:y","note":"n","edges":[{"rationale_uid":"r2","target_entity_id":"t2","target_path":"p2","repo_id":"repo2","comment_kind":"caveat"}]}`
	path := filepath.Join(t.TempDir(), "trailing.json")
	if err := os.WriteFile(path, []byte(first+second), 0o600); err != nil {
		t.Fatalf("write temp fixture: %v", err)
	}
	if _, err := loadRationaleExpectedEdges(path); err == nil {
		t.Fatal("loadRationaleExpectedEdges accepted a fixture with a second, concatenated JSON object trailing the first")
	}
}

// TestRationaleEdgeKeyIsInjective is the regression test for
// rationaleEdgeKey's join delimiter. It joined seven fields with a raw "\x00",
// so a value that itself contains "\x00" shifts the field boundary: two
// structurally distinct edges can render the identical key.
//
// Unlike codeCallEdgeKey's "|" (a legal, plausible POSIX path byte), a raw
// NUL cannot appear in TargetPath -- POSIX path syscalls reject it outright
// -- and is not a realistic byte for the other six fields as this extractor
// currently produces them. This test constructs the collision directly
// rather than pointing to a live input that produces one, because none does
// today. The fix (writeLengthPrefixedField, shared with Key(),
// sqlRelationshipEdgeKey, and codeCallEdgeKey) closes the gap regardless: it
// is provably injective for any byte content, not merely for content that
// avoids one reserved delimiter, so this key no longer depends on an
// assumption about what the extractor happens to emit today.
func TestRationaleEdgeKeyIsInjective(t *testing.T) {
	t.Parallel()

	keyA := rationaleEdgeKey(rationaleExpectedEdge{
		RationaleUID: "A\x00B", TargetEntityID: "C", TargetPath: "D", RepoID: "E", CommentKind: "F",
	})
	keyB := rationaleEdgeKey(rationaleExpectedEdge{
		RationaleUID: "A", TargetEntityID: "B\x00C", TargetPath: "D", RepoID: "E", CommentKind: "F",
	})
	if keyA == keyB {
		t.Fatalf("distinct rationale edges collided onto one key %q; a boundary-shifting value would silently merge them in compareRationaleExpectedSets", keyA)
	}
}

func rationaleFixtureComments(value any) []map[string]any {
	items, _ := value.([]any)
	comments := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if comment, ok := item.(map[string]any); ok {
			comments = append(comments, comment)
		}
	}
	return comments
}

func writeRationaleExpectedEdges(t *testing.T, file rationaleExpectedEdgesFile) string {
	t.Helper()
	raw, err := json.Marshal(file)
	if err != nil {
		t.Fatalf("marshal expected edges: %v", err)
	}
	path := filepath.Join(t.TempDir(), "expected-edges.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write expected edges: %v", err)
	}
	return path
}
