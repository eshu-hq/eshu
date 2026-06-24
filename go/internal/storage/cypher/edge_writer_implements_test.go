// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

func TestBuildCodeCallRowMapRoutesInstantiates(t *testing.T) {
	payload := map[string]any{
		"caller_entity_id":   "uid:caller",
		"callee_entity_id":   "uid:class",
		"caller_entity_type": "Function",
		"callee_entity_type": "Class",
		"call_kind":          "constructor_call",
		"relationship_type":  "INSTANTIATES",
		"resolution_method":  "type_inferred",
	}
	cypher, rowMap, ok := buildCodeCallRowMap(payload, "parser/code-calls")
	if !ok {
		t.Fatal("buildCodeCallRowMap ok = false, want true")
	}
	if !strings.Contains(cypher, "rel:INSTANTIATES") {
		t.Errorf("template does not write INSTANTIATES: %q", cypher)
	}
	if !strings.Contains(cypher, "source:Function") || !strings.Contains(cypher, "target:Class") {
		t.Errorf("template does not use exact INSTANTIATES endpoint labels: %q", cypher)
	}
	if rowMap["confidence"] != 0.80 {
		t.Errorf("confidence = %#v, want 0.80 (type_inferred)", rowMap["confidence"])
	}
}

func TestBuildCodeCallRowMapRoutesInstantiatesCanonicalFallback(t *testing.T) {
	payload := map[string]any{
		"caller_entity_id":  "uid:caller",
		"callee_entity_id":  "uid:class",
		"relationship_type": "INSTANTIATES",
	}
	cypher, _, ok := buildCodeCallRowMap(payload, "parser/code-calls")
	if !ok {
		t.Fatal("buildCodeCallRowMap ok = false, want true")
	}
	if cypher != batchCanonicalInstantiatesUpsertCypher {
		t.Errorf("expected fallback INSTANTIATES template, got %q", cypher)
	}
}

func TestRetractCodeCallEdgesCoversInstantiates(t *testing.T) {
	if !strings.Contains(retractCodeCallParserEdgesCypher, "INSTANTIATES") {
		t.Error("parser code-call retract does not clean stale INSTANTIATES edges")
	}
	if !strings.Contains(retractCodeCallFallbackEdgesCypher, "INSTANTIATES") {
		t.Error("fallback code-call retract does not clean stale INSTANTIATES edges")
	}
}
