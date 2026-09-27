// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

func TestPortContentStoreListRepoEntitiesByKeysBoundsEachUniqueKey(t *testing.T) {
	t.Parallel()

	first := querycontract.EntityContentKey{RelativePath: "a.go", EntityType: "Function", EntityName: "decode", StartLine: 7}
	second := querycontract.EntityContentKey{RelativePath: "b.go", EntityType: "Function", EntityName: "decode", StartLine: 8}
	missing := querycontract.EntityContentKey{RelativePath: "missing.go", EntityType: "Function", EntityName: "decode", StartLine: 9}
	store := content.FakePortContentStore{Entities: []querycontract.EntityContent{
		{EntityID: "other-repo", RepoID: "repo-2", RelativePath: "a.go", EntityType: "Function", EntityName: "decode", StartLine: 7},
		{EntityID: "wrong-line", RepoID: "repo-1", RelativePath: "a.go", EntityType: "Function", EntityName: "decode", StartLine: 8},
		{EntityID: "c", RepoID: "repo-1", RelativePath: "a.go", EntityType: "Function", EntityName: "decode", StartLine: 7},
		{EntityID: "b", RepoID: "repo-1", RelativePath: "a.go", EntityType: "Function", EntityName: "decode", StartLine: 7},
		{EntityID: "a", RepoID: "repo-1", RelativePath: "a.go", EntityType: "Function", EntityName: "decode", StartLine: 7},
		{EntityID: "second", RepoID: "repo-1", RelativePath: "b.go", EntityType: "Function", EntityName: "decode", StartLine: 8},
	}}

	got, err := store.ListRepoEntitiesByKeys(t.Context(), "repo-1", []querycontract.EntityContentKey{second, first, first, missing})
	if err != nil {
		t.Fatalf("ListRepoEntitiesByKeys() error = %v", err)
	}
	want := []string{"second", "a", "b"}
	if len(got) != len(want) {
		t.Fatalf("ListRepoEntitiesByKeys() returned %d rows, want %d: %+v", len(got), len(want), got)
	}
	for i, id := range want {
		if got[i].EntityID != id {
			t.Fatalf("row %d ID = %q, want %q", i, got[i].EntityID, id)
		}
	}
}

func TestPortContentStoreListRepoEntitiesByKeysRejectsOversizeBatch(t *testing.T) {
	t.Parallel()
	keys := make([]querycontract.EntityContentKey, querycontract.MaxEntityContentKeys+1)
	for i := range keys {
		keys[i].StartLine = i + 1
	}
	store := content.FakePortContentStore{}
	if _, err := store.ListRepoEntitiesByKeys(t.Context(), "repo-1", keys[:querycontract.MaxEntityContentKeys]); err != nil {
		t.Fatalf("maximum-size batch error = %v", err)
	}
	_, err := store.ListRepoEntitiesByKeys(t.Context(), "repo-1", keys)
	if err == nil || !strings.Contains(err.Error(), strconv.Itoa(querycontract.MaxEntityContentKeys)) {
		t.Fatalf("oversize batch error = %v, want %d-key bound", err, querycontract.MaxEntityContentKeys)
	}
}

func TestPortContentStoreListRepoEntitiesByKeysHonorsCanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	store := content.FakePortContentStore{}
	_, err := store.ListRepoEntitiesByKeys(ctx, "repo-1", []querycontract.EntityContentKey{{RelativePath: "a.go"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled fake read error = %v, want context canceled", err)
	}
}
