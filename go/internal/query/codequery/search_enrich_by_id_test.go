// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

type idEnrichmentStore struct {
	content.FakePortContentStore
	rows        []querycontract.EntityContent
	listCalls   int
	searchCalls int
	gotRepo     string
	gotIDs      []string
	gotLimit    int
}

func (s *idEnrichmentStore) ListRepoEntitiesByIDs(_ context.Context, repoID string, ids []string, limit int) ([]querycontract.EntityContent, error) {
	s.listCalls++
	s.gotRepo, s.gotIDs, s.gotLimit = repoID, append([]string(nil), ids...), limit
	return append([]querycontract.EntityContent(nil), s.rows...), nil
}

func (s *idEnrichmentStore) SearchEntityContent(_ context.Context, _ string, _ string, _ int) ([]querycontract.EntityContent, error) {
	s.searchCalls++
	return append([]querycontract.EntityContent(nil), s.rows...), nil
}

func TestGraphSearchEnrichesOnlyMatchingEntityIDsInOneRead(t *testing.T) {
	t.Parallel()

	store := &idEnrichmentStore{rows: []querycontract.EntityContent{
		{EntityID: "entity-1", RepoID: "repo-1", RelativePath: "src/one.py", EntityType: "Function", EntityName: "decode", StartLine: 1, Metadata: map[string]any{"docstring": "correct"}},
		{EntityID: "other-entity", RepoID: "repo-1", RelativePath: "src/three.py", EntityType: "Function", EntityName: "decode", StartLine: 3, Metadata: map[string]any{"docstring": "wrong"}},
	}}
	handler := &CodeHandler{Content: store}
	results := []map[string]any{
		{"entity_id": "entity-1", "repo_id": "repo-1", "name": "decode", "labels": []string{"Function"}, "file_path": "src/one.py", "start_line": 1},
		{"entity_id": "entity-2", "repo_id": "repo-1", "name": "decode", "labels": []string{"Function"}, "file_path": "src/two.py", "start_line": 2, "metadata": map[string]any{"docstring": "already present"}},
		{"entity_id": "entity-3", "repo_id": "repo-1", "name": "decode", "labels": []string{"Function"}, "file_path": "src/three.py", "start_line": 3},
	}

	got, err := handler.enrichGraphSearchResultsWithContentMetadata(context.Background(), results, "repo-1")
	if err != nil {
		t.Fatalf("enrich graph search: %v", err)
	}
	if store.listCalls != 1 || store.searchCalls != 0 {
		t.Fatalf("batch reads = %d, substring reads = %d; want 1 and 0", store.listCalls, store.searchCalls)
	}
	if store.gotRepo != "repo-1" || store.gotLimit != 2 || !reflect.DeepEqual(store.gotIDs, []string{"entity-1", "entity-3"}) {
		t.Fatalf("batch request = (%q, %v, %d), want (repo-1, [entity-1 entity-3], 2)", store.gotRepo, store.gotIDs, store.gotLimit)
	}
	if got := got[0]["metadata"].(map[string]any)["docstring"]; got != "correct" {
		t.Fatalf("first result docstring = %v, want correct", got)
	}
	if got := got[1]["metadata"].(map[string]any)["docstring"]; got != "already present" {
		t.Fatalf("second result docstring = %v, want existing value", got)
	}
	if _, ok := got[2]["metadata"]; ok {
		t.Fatalf("third result received metadata from a different entity: %v", got[2])
	}
}
