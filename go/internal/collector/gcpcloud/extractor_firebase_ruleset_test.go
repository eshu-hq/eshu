// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"testing"
)

func fixtureFirebaseRulesetData(t *testing.T) []byte {
	t.Helper()
	blob := map[string]any{
		"name": "projects/demo-project/rulesets/abc123",
		"source": map[string]any{
			"files": []any{
				map[string]any{"name": "firestore.rules", "content": "rules_version='2';service cloud.firestore{}"},
				map[string]any{"name": "shared.rules", "content": "function foo(){return true;}"},
			},
		},
		"createTime": "2024-06-01T00:00:00Z",
		"metadata":   map[string]any{"services": []any{"cloud.firestore"}},
	}
	raw, err := json.Marshal(blob)
	if err != nil {
		t.Fatalf("marshal firebase ruleset fixture: %v", err)
	}
	return raw
}

func firebaseRulesetCtx(t *testing.T) ExtractContext {
	t.Helper()
	return ExtractContext{
		FullResourceName: "//firebaserules.googleapis.com/projects/demo-project/rulesets/abc123",
		AssetType:        firebaseRulesetAssetType,
		Data:             fixtureFirebaseRulesetData(t),
	}
}

// TestExtractFirebaseRulesetAttributes proves the bounded attribute set is
// surfaced and that raw rule source content is never persisted.
func TestExtractFirebaseRulesetAttributes(t *testing.T) {
	got, err := extractFirebaseRuleset(firebaseRulesetCtx(t))
	if err != nil {
		t.Fatalf("extractFirebaseRuleset: %v", err)
	}
	if got.Attributes["source_file_count"] != 2 {
		t.Errorf("source_file_count = %v, want 2", got.Attributes["source_file_count"])
	}
	if got.Attributes["creation_time"] == nil {
		t.Errorf("creation_time missing")
	}
	services, ok := got.Attributes["services"].([]string)
	if !ok || len(services) != 1 || services[0] != "cloud.firestore" {
		t.Errorf("services = %#v, want [cloud.firestore]", got.Attributes["services"])
	}
	// Raw rule source content must never leave the parser.
	blob, _ := json.Marshal(got)
	for _, token := range []string{"rules_version", "function foo", "return true", "content"} {
		if containsString(string(blob), token) {
			t.Fatalf("extraction leaked raw rule source token %q", token)
		}
	}
}

// TestExtractFirebaseRulesetProjectEdge proves the ruleset resolves a typed edge
// to its parent Firebase project.
func TestExtractFirebaseRulesetProjectEdge(t *testing.T) {
	got, err := extractFirebaseRuleset(firebaseRulesetCtx(t))
	if err != nil {
		t.Fatalf("extractFirebaseRuleset: %v", err)
	}
	var edge *RelationshipObservation
	for i := range got.Relationships {
		if got.Relationships[i].RelationshipType == relationshipTypeFirebaseRulesetBelongsToProject {
			edge = &got.Relationships[i]
		}
	}
	if edge == nil {
		t.Fatalf("missing belongs_to_project edge: %#v", got.Relationships)
	}
	if edge.TargetFullResourceName != "//firebase.googleapis.com/projects/demo-project" {
		t.Errorf("project target = %q, want canonical FirebaseProject name", edge.TargetFullResourceName)
	}
	if edge.TargetAssetType != firebaseProjectAssetType {
		t.Errorf("project target asset type = %q", edge.TargetAssetType)
	}
}

// TestExtractFirebaseRulesetNoProjectNoEdge proves that when the full resource
// name carries no derivable project, no unresolvable edge is emitted.
func TestExtractFirebaseRulesetNoProjectNoEdge(t *testing.T) {
	got, err := extractFirebaseRuleset(ExtractContext{
		FullResourceName: "//firebaserules.googleapis.com/rulesets/orphan",
		AssetType:        firebaseRulesetAssetType,
		Data:             []byte(`{"createTime":"2024-06-01T00:00:00Z"}`),
	})
	if err != nil {
		t.Fatalf("extractFirebaseRuleset: %v", err)
	}
	for _, rel := range got.Relationships {
		if rel.RelationshipType == relationshipTypeFirebaseRulesetBelongsToProject {
			t.Errorf("emitted project edge without a derivable project: %#v", rel)
		}
	}
}

// TestExtractFirebaseRulesetEmpty proves an empty blob yields no source count and
// no panic, with the project edge still derived from the full resource name.
func TestExtractFirebaseRulesetEmpty(t *testing.T) {
	got, err := extractFirebaseRuleset(ExtractContext{
		FullResourceName: "//firebaserules.googleapis.com/projects/demo-project/rulesets/x",
		AssetType:        firebaseRulesetAssetType,
		Data:             []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("extractFirebaseRuleset empty: %v", err)
	}
	if _, ok := got.Attributes["source_file_count"]; ok {
		t.Errorf("empty blob reported source_file_count: %#v", got.Attributes)
	}
	if len(got.Relationships) != 1 {
		t.Errorf("edges = %d, want 1 (project edge from full name)", len(got.Relationships))
	}
}

// TestExtractFirebaseRulesetMalformed proves malformed JSON is a decode error.
func TestExtractFirebaseRulesetMalformed(t *testing.T) {
	_, err := extractFirebaseRuleset(ExtractContext{
		FullResourceName: "//firebaserules.googleapis.com/projects/demo-project/rulesets/x",
		AssetType:        firebaseRulesetAssetType,
		Data:             []byte(`{bad`),
	})
	if err == nil {
		t.Fatalf("expected decode error for malformed data")
	}
}

// TestFirebaseRulesetExtractorRegistered proves the asset type is wired into the
// shared registry.
func TestFirebaseRulesetExtractorRegistered(t *testing.T) {
	if !HasAssetExtractor(firebaseRulesetAssetType) {
		t.Fatalf("firebase ruleset extractor not registered for %q", firebaseRulesetAssetType)
	}
}
