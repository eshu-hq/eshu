// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRationaleExpectedEdgeRecordsValidatesCommittedFixture(t *testing.T) {
	t.Parallel()

	repoID, records, err := LoadRationaleExpectedEdgeRecords(rationaleFamilyExpectedEdgesPath(repoRootDir(t)))
	if err != nil {
		t.Fatalf("LoadRationaleExpectedEdgeRecords: %v", err)
	}
	if repoID != rationaleFamilyRepoID {
		t.Fatalf("repo ID = %q, want %q", repoID, rationaleFamilyRepoID)
	}
	if len(records) != 3 {
		t.Fatalf("record count = %d, want 3", len(records))
	}
	for i, record := range records {
		if record.RelationshipType != "EXPLAINS" {
			t.Errorf("record %d type = %q, want EXPLAINS", i, record.RelationshipType)
		}
		if len(record.SourceRecord.Labels) == 0 || len(record.TargetRecord.Labels) == 0 {
			t.Errorf("record %d has blank endpoint labels", i)
		}
		if record.SourceRecord.Props == nil || record.EdgeProps == nil || record.TargetRecord.Props == nil {
			t.Errorf("record %d omits a full property map", i)
		}
	}
}

func TestLoadRationaleExpectedEdgeRecordsRejectsIdentityAndScopeDrift(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(rationaleFamilyExpectedEdgesPath(repoRootDir(t)))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var base map[string]any
	if err := json.Unmarshal(raw, &base); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	tests := map[string]func(map[string]any){
		"blank top-level repo": func(doc map[string]any) { edgeAt(t, doc, 0)["repo_id"] = "" },
		"mixed repositories":   func(doc map[string]any) { edgeAt(t, doc, 1)["repo_id"] = "repository:other" },
		"top-level source identity mismatch": func(doc map[string]any) {
			edgeAt(t, doc, 0)["source_entity_id"] = "rationale:wrong"
		},
		"source identity mismatch": func(doc map[string]any) {
			source := edgeAt(t, doc, 0)["source_record"].(map[string]any)
			source["props"].(map[string]any)["uid"] = "rationale:wrong"
		},
		"source repository mismatch": func(doc map[string]any) {
			source := edgeAt(t, doc, 0)["source_record"].(map[string]any)
			source["props"].(map[string]any)["repo_id"] = "repository:other"
		},
		"target identity mismatch": func(doc map[string]any) {
			target := edgeAt(t, doc, 0)["target_record"].(map[string]any)
			target["props"].(map[string]any)["uid"] = "content-entity:wrong"
		},
		"target id mismatch": func(doc map[string]any) {
			target := edgeAt(t, doc, 0)["target_record"].(map[string]any)
			target["props"].(map[string]any)["id"] = "content-entity:wrong"
		},
		"target repository mismatch": func(doc map[string]any) {
			target := edgeAt(t, doc, 0)["target_record"].(map[string]any)
			target["props"].(map[string]any)["repo_id"] = "repository:other"
		},
		"target path mismatch": func(doc map[string]any) {
			target := edgeAt(t, doc, 0)["target_record"].(map[string]any)
			target["props"].(map[string]any)["relative_path"] = "wrong.py"
		},
		"edge kind mismatch": func(doc map[string]any) {
			edgeAt(t, doc, 0)["edge_props"].(map[string]any)["comment_kind"] = "HACK"
		},
		"missing source record": func(doc map[string]any) { delete(edgeAt(t, doc, 0), "source_record") },
		"missing edge props":    func(doc map[string]any) { delete(edgeAt(t, doc, 0), "edge_props") },
		"missing target record": func(doc map[string]any) { delete(edgeAt(t, doc, 0), "target_record") },
	}

	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := cloneJSONDocument(t, base)
			mutate(doc)
			path := writeJSONDocument(t, doc)
			if _, _, err := LoadRationaleExpectedEdgeRecords(path); err == nil {
				t.Fatal("LoadRationaleExpectedEdgeRecords = nil error, want fail-closed validation")
			}
		})
	}
}

func edgeAt(t *testing.T, doc map[string]any, index int) map[string]any {
	t.Helper()
	edges, ok := doc["edges"].([]any)
	if !ok || index >= len(edges) {
		t.Fatalf("fixture edges have unexpected shape: %#v", doc["edges"])
	}
	edge, ok := edges[index].(map[string]any)
	if !ok {
		t.Fatalf("fixture edge %d has unexpected shape: %#v", index, edges[index])
	}
	return edge
}

func cloneJSONDocument(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal fixture clone: %v", err)
	}
	var clone map[string]any
	if err := json.Unmarshal(raw, &clone); err != nil {
		t.Fatalf("unmarshal fixture clone: %v", err)
	}
	return clone
}

func writeJSONDocument(t *testing.T, value map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	path := filepath.Join(t.TempDir(), strings.ReplaceAll(t.Name(), "/", "-")+".json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}
