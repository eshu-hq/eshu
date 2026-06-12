package cypher

import (
	"strings"
	"testing"
)

func TestBuildDocumentationRowMapRoutesEntityTarget(t *testing.T) {
	payload := map[string]any{
		"section_uid":      "docsec:1",
		"target_entity_id": "uid:func",
		"scope_id":         "scope-1",
		"document_id":      "doc-1",
		"section_id":       "sec-1",
		"heading_text":     "Runbook",
		"target_kind":      "entity",
		"mention_kind":     "code_symbol",
	}
	cypher, rowMap, ok := buildDocumentationRowMap(payload, "reducer/documentation")
	if !ok {
		t.Fatal("buildDocumentationRowMap ok = false, want true")
	}
	if cypher != batchCanonicalDocumentationEntityEdgeCypher {
		t.Errorf("expected entity edge template, got %q", cypher)
	}
	if !strings.Contains(cypher, "rel:DOCUMENTS") || !strings.Contains(cypher, "MERGE (section:DocumentationSection") {
		t.Errorf("template missing DOCUMENTS edge / section node: %q", cypher)
	}
	if rowMap["scope_id"] != "scope-1" || rowMap["heading_text"] != "Runbook" {
		t.Errorf("rowMap identity fields not carried: %#v", rowMap)
	}
}

func TestBuildDocumentationRowMapRoutesWorkloadTarget(t *testing.T) {
	payload := map[string]any{
		"section_uid":      "docsec:1",
		"target_entity_id": "wl-1",
		"scope_id":         "scope-1",
		"target_kind":      "workload",
	}
	cypher, _, ok := buildDocumentationRowMap(payload, "reducer/documentation")
	if !ok {
		t.Fatal("buildDocumentationRowMap ok = false, want true")
	}
	if cypher != batchCanonicalDocumentationWorkloadEdgeCypher {
		t.Errorf("expected workload edge template, got %q", cypher)
	}
	if !strings.Contains(cypher, "MATCH (target:Workload {id: row.target_entity_id})") {
		t.Errorf("workload template does not match Workload by id: %q", cypher)
	}
}

func TestBuildDocumentationRowMapDropsServiceTarget(t *testing.T) {
	payload := map[string]any{
		"section_uid":      "docsec:1",
		"target_entity_id": "svc-1",
		"target_kind":      "service",
	}
	if _, _, ok := buildDocumentationRowMap(payload, "reducer/documentation"); ok {
		t.Error("service target should be dropped (no Service node), got ok=true")
	}
}

func TestBuildDocumentationRowMapRequiresSectionAndTarget(t *testing.T) {
	if _, _, ok := buildDocumentationRowMap(map[string]any{"target_entity_id": "uid:func"}, "src"); ok {
		t.Error("missing section_uid should be rejected")
	}
	if _, _, ok := buildDocumentationRowMap(map[string]any{"section_uid": "docsec:1"}, "src"); ok {
		t.Error("missing target_entity_id should be rejected")
	}
}

func TestRetractDocumentationEdgesIsScopeScoped(t *testing.T) {
	stmt := BuildRetractDocumentationEdges([]string{"scope-1"}, "reducer/documentation")
	if !strings.Contains(stmt.Cypher, "rel:DOCUMENTS") {
		t.Errorf("retract does not target DOCUMENTS: %q", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "section.scope_id IN $scope_ids") {
		t.Errorf("retract is not scope-scoped: %q", stmt.Cypher)
	}
	if _, ok := stmt.Parameters["scope_ids"]; !ok {
		t.Error("retract missing scope_ids parameter")
	}
}
