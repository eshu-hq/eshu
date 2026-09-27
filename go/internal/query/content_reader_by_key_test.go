// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func TestContentReaderListRepoEntitiesByKeysQueriesEachUniqueKey(t *testing.T) {
	t.Parallel()

	keys := []querycontract.EntityContentKey{
		{RelativePath: "second.go", EntityType: "Function", EntityName: "decode", StartLine: 8},
		{RelativePath: "first.go", EntityType: "Function", EntityName: "a", StartLine: 7},
		{RelativePath: "first.go", EntityType: "Function", EntityName: "a", StartLine: 7},
	}
	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns: []string{"entity_id", "repo_id", "relative_path", "entity_type", "entity_name", "start_line", "end_line", "language", "source_cache", "metadata"},
		rows: [][]driver.Value{
			{"second", "repo-1", "second.go", "Function", "decode", int64(8), int64(10), "go", "", []byte(`{"semantic_summary":"two"}`)},
			{"first-a", "repo-1", "first.go", "Function", "a", int64(7), int64(9), "go", "", []byte(`{"semantic_summary":"one"}`)},
			{"first-b", "repo-1", "first.go", "Function", "a", int64(7), int64(9), "go", "", []byte(`{}`)},
		},
		queryContainsInOrder: []string{
			"WITH keys(relative_path, entity_type, entity_name, start_line, key_order) AS (",
			"VALUES ($2::text, $3::text, $4::text, $5::integer, 0),",
			"($6::text, $7::text, $8::text, $9::integer, 1)",
			"JOIN LATERAL",
			"FROM content_entities",
			"WHERE repo_id = $1",
			"AND relative_path = keys.relative_path",
			"AND entity_type = keys.entity_type",
			"AND entity_name = keys.entity_name",
			"AND start_line = keys.start_line",
			"ORDER BY entity_id",
			"LIMIT 2",
			"ORDER BY keys.key_order, hit.entity_id",
		},
		wantArgs: []driver.Value{"repo-1", "second.go", "Function", "decode", int64(8), "first.go", "Function", "a", int64(7)},
	}})
	reader := NewContentReader(db)
	got, err := reader.ListRepoEntitiesByKeys(context.Background(), "repo-1", keys)
	if err != nil {
		t.Fatalf("ListRepoEntitiesByKeys() error = %v", err)
	}
	if len(got) != 3 || got[0].EntityID != "second" || got[1].EntityID != "first-a" || got[2].EntityID != "first-b" {
		t.Fatalf("ListRepoEntitiesByKeys() = %+v, want both keys in input order and two tied rows", got)
	}
	if got[0].Metadata["semantic_summary"] != "two" {
		t.Fatalf("metadata not decoded: %+v", got[0].Metadata)
	}
}

func TestContentReaderListRepoEntitiesByKeysRejectsOversizeBatch(t *testing.T) {
	t.Parallel()
	reader := NewContentReader(openContentReaderTestDB(t, nil))
	keys := make([]querycontract.EntityContentKey, querycontract.MaxEntityContentKeys+1)
	for i := range keys {
		keys[i].StartLine = i + 1
	}
	_, err := reader.ListRepoEntitiesByKeys(context.Background(), "repo-1", keys)
	if err == nil || !strings.Contains(err.Error(), strconv.Itoa(querycontract.MaxEntityContentKeys)) {
		t.Fatalf("oversize batch error = %v, want explicit %d-key bound", err, querycontract.MaxEntityContentKeys)
	}
}

func TestContentReaderListRepoEntitiesByKeysPropagatesQueryError(t *testing.T) {
	t.Parallel()
	want := errors.New("query interrupted")
	db := openContentReaderTestDB(t, []contentReaderQueryResult{{err: want}})
	reader := NewContentReader(db)
	_, err := reader.ListRepoEntitiesByKeys(context.Background(), "repo-1", []querycontract.EntityContentKey{{RelativePath: "a.go", EntityType: "Function", EntityName: "a", StartLine: 1}})
	if !errors.Is(err, want) {
		t.Fatalf("query error = %v, want %v", err, want)
	}
}

func TestContentReaderListRepoEntitiesByKeysHonorsCanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := NewContentReader(openContentReaderTestDB(t, nil))
	_, err := reader.ListRepoEntitiesByKeys(ctx, "repo-1", []querycontract.EntityContentKey{{RelativePath: "a.go", EntityType: "Function", EntityName: "a", StartLine: 1}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read error = %v, want context canceled", err)
	}
}

func TestContentReaderListRepoEntitiesByKeysEmptyBatchSkipsQuery(t *testing.T) {
	t.Parallel()
	reader := NewContentReader(openContentReaderTestDB(t, nil))
	got, err := reader.ListRepoEntitiesByKeys(context.Background(), "repo-1", nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty batch = (%+v, %v), want empty nil", got, err)
	}
}
