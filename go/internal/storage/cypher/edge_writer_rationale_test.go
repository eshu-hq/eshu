package cypher

import (
	"strings"
	"testing"
)

func TestBuildRationaleRowMapRoutesExplainsEdge(t *testing.T) {
	payload := map[string]any{
		"rationale_uid":    "rationale:uid:func:HACK:abc",
		"target_entity_id": "uid:func",
		"repo_id":          "repo-1",
		"comment_kind":     "HACK",
		"excerpt_hash":     "abc",
	}
	cypher, rowMap, ok := buildRationaleRowMap(payload, "reducer/rationale")
	if !ok {
		t.Fatal("buildRationaleRowMap ok = false, want true")
	}
	if cypher != batchCanonicalRationaleExplainsEdgeCypher {
		t.Errorf("expected EXPLAINS template, got %q", cypher)
	}
	if !strings.Contains(cypher, "rel:EXPLAINS") || !strings.Contains(cypher, "MERGE (rationale:Rationale") {
		t.Errorf("template missing EXPLAINS edge / Rationale node: %q", cypher)
	}
	if rowMap["comment_kind"] != "HACK" || rowMap["repo_id"] != "repo-1" {
		t.Errorf("rowMap fields not carried: %#v", rowMap)
	}
}

func TestBuildRationaleRowMapRequiresRationaleAndTarget(t *testing.T) {
	if _, _, ok := buildRationaleRowMap(map[string]any{"target_entity_id": "uid:func"}, "src"); ok {
		t.Error("missing rationale_uid should be rejected")
	}
	if _, _, ok := buildRationaleRowMap(map[string]any{"rationale_uid": "r"}, "src"); ok {
		t.Error("missing target_entity_id should be rejected")
	}
}

func TestRetractRationaleEdgesIsRepoScoped(t *testing.T) {
	stmt := BuildRetractRationaleEdges([]string{"repo-1"}, "reducer/rationale")
	if !strings.Contains(stmt.Cypher, "rel:EXPLAINS") {
		t.Errorf("retract does not target EXPLAINS: %q", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "rationale.repo_id IN $repo_ids") {
		t.Errorf("retract is not repo-scoped: %q", stmt.Cypher)
	}
}
