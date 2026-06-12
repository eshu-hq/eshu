package cypher

import (
	"strings"
	"testing"
)

func TestBuildInheritanceRowMapRoutesImplementsLabelScoped(t *testing.T) {
	payload := map[string]any{
		"child_entity_id":    "uid:impl",
		"parent_entity_id":   "uid:iface",
		"child_entity_type":  "Class",
		"parent_entity_type": "Interface",
		"relationship_type":  "IMPLEMENTS",
	}
	cypher, rowMap, ok := buildInheritanceRowMap(payload, "reducer/inheritance")
	if !ok {
		t.Fatal("buildInheritanceRowMap ok = false, want true")
	}
	if !strings.Contains(cypher, "rel:IMPLEMENTS") {
		t.Errorf("cypher does not write IMPLEMENTS: %q", cypher)
	}
	if !strings.Contains(cypher, "child:Class") || !strings.Contains(cypher, "parent:Interface") {
		t.Errorf("label-scoped cypher missing exact endpoint labels: %q", cypher)
	}
	if rowMap["relationship_type"] != "IMPLEMENTS" {
		t.Errorf("rowMap relationship_type = %#v, want IMPLEMENTS", rowMap["relationship_type"])
	}
}

func TestBuildInheritanceRowMapRoutesImplementsCanonicalFallback(t *testing.T) {
	payload := map[string]any{
		"child_entity_id":   "uid:impl",
		"parent_entity_id":  "uid:iface",
		"relationship_type": "IMPLEMENTS",
	}
	cypher, _, ok := buildInheritanceRowMap(payload, "reducer/inheritance")
	if !ok {
		t.Fatal("buildInheritanceRowMap ok = false, want true")
	}
	if cypher != batchCanonicalImplementsEdgeUpsertCypher {
		t.Errorf("expected canonical IMPLEMENTS template fallback, got %q", cypher)
	}
}

func TestRetractInheritanceEdgesCoversImplements(t *testing.T) {
	if !strings.Contains(retractInheritanceEdgesCypher, "IMPLEMENTS") {
		t.Error("inheritance retract does not clean stale IMPLEMENTS edges")
	}
}
