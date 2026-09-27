// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type boundedMetadataStore struct {
	fakePortContentStore
}

func (boundedMetadataStore) SearchEntityContent(context.Context, string, string, int) ([]EntityContent, error) {
	return nil, errors.New("resolver metadata must not use substring search")
}

func TestEntityMetadataHydrationUsesBoundedIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		graphID    string
		contentID  string
		entityName string
	}{
		{name: "canonical ID and common name", graphID: "content-decode", contentID: "content-decode", entityName: "decode"},
		{name: "legacy ID and short name", graphID: "graph-a", contentID: "content-a", entityName: "a"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := boundedMetadataStore{fakePortContentStore: fakePortContentStore{entities: []EntityContent{{
				EntityID:     tc.contentID,
				RepoID:       "repo-1",
				RelativePath: "src/example.php",
				EntityType:   "Function",
				EntityName:   tc.entityName,
				StartLine:    12,
				Metadata:     map[string]any{"marker": tc.name},
			}}}}
			results := []map[string]any{{
				"id":         tc.graphID,
				"name":       tc.entityName,
				"labels":     []string{"Function"},
				"file_path":  "src/example.php",
				"repo_id":    "repo-1",
				"start_line": 12,
			}}
			handler := &EntityHandler{Content: store}
			got, err := handler.EnrichEntityResultsWithContentMetadata(t.Context(), results, "repo-1", tc.entityName, 2)
			if err != nil {
				t.Fatalf("enrich metadata: %v", err)
			}
			if len(got) != 1 || got[0]["id"] != tc.graphID {
				t.Fatalf("graph row changed: %#v", got)
			}
			metadata, ok := got[0]["metadata"].(map[string]any)
			if !ok || metadata["marker"] != tc.name {
				t.Fatalf("metadata = %#v, want marker %q", got[0]["metadata"], tc.name)
			}
		})
	}
}

func TestEntityMetadataHydrationRejectsAmbiguousLegacyKey(t *testing.T) {
	t.Parallel()

	store := boundedMetadataStore{fakePortContentStore: fakePortContentStore{entities: []EntityContent{
		{EntityID: "content-1", RepoID: "repo-1", RelativePath: "src/example.php", EntityType: "Function", EntityName: "decode", StartLine: 12, Metadata: map[string]any{"marker": "one"}},
		{EntityID: "content-2", RepoID: "repo-1", RelativePath: "src/example.php", EntityType: "Function", EntityName: "decode", StartLine: 12, Metadata: map[string]any{"marker": "two"}},
	}}}
	handler := &EntityHandler{Content: store}
	results := []map[string]any{{
		"id": "legacy", "name": "decode", "labels": []string{"Function"},
		"file_path": "src/example.php", "repo_id": "repo-1", "start_line": 12,
	}}
	if _, err := handler.EnrichEntityResultsWithContentMetadata(t.Context(), results, "repo-1", "decode", 2); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous exact-key hydration error, got %v", err)
	}
}

func TestEntityMetadataHydrationDoesNotBorrowMetadataForIDHit(t *testing.T) {
	t.Parallel()

	store := boundedMetadataStore{fakePortContentStore: fakePortContentStore{entities: []EntityContent{
		{EntityID: "content-1", RepoID: "repo-1", RelativePath: "src/example.php", EntityType: "Function", EntityName: "decode", StartLine: 12},
		{EntityID: "content-2", RepoID: "repo-1", RelativePath: "src/example.php", EntityType: "Function", EntityName: "decode", StartLine: 12, Metadata: map[string]any{"marker": "unrelated"}},
	}}}
	handler := &EntityHandler{Content: store}
	results := []map[string]any{{
		"id": "content-1", "name": "decode", "labels": []string{"Function"},
		"file_path": "src/example.php", "repo_id": "repo-1", "start_line": 12,
	}}
	got, err := handler.EnrichEntityResultsWithContentMetadata(t.Context(), results, "repo-1", "decode", 2)
	if err != nil {
		t.Fatalf("enrich ID hit: %v", err)
	}
	if _, ok := got[0]["metadata"]; ok {
		t.Fatalf("borrowed metadata from another entity: %#v", got[0]["metadata"])
	}
}

func TestEntityMetadataHydrationRejectsContradictoryIDTuple(t *testing.T) {
	t.Parallel()

	store := boundedMetadataStore{fakePortContentStore: fakePortContentStore{entities: []EntityContent{
		{EntityID: "graph-1", RepoID: "repo-1", RelativePath: "src/other.php", EntityType: "Function", EntityName: "decode", StartLine: 12, Metadata: map[string]any{"marker": "wrong"}},
		{EntityID: "content-1", RepoID: "repo-1", RelativePath: "src/example.php", EntityType: "Function", EntityName: "decode", StartLine: 12, Metadata: map[string]any{"marker": "right"}},
	}}}
	handler := &EntityHandler{Content: store}
	results := []map[string]any{{
		"id": "graph-1", "name": "decode", "labels": []string{"Function"},
		"file_path": "src/example.php", "repo_id": "repo-1", "start_line": 12,
	}}
	got, err := handler.EnrichEntityResultsWithContentMetadata(t.Context(), results, "repo-1", "decode", 2)
	if err != nil {
		t.Fatalf("enrich contradictory ID: %v", err)
	}
	metadata, ok := got[0]["metadata"].(map[string]any)
	if !ok || metadata["marker"] != "right" {
		t.Fatalf("metadata = %#v, want exact tuple row", got[0]["metadata"])
	}
}
