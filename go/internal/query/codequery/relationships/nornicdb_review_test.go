// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/relationships"
)

// TestNornicDBOneHopRelationshipsSignalsTruncation proves the row ceiling clips
// the result AND reports it, so the exact-truth envelope never silently drops
// edges for a high-degree symbol (#5730 review, Codex P1).
func TestNornicDBOneHopRelationshipsSignalsTruncation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		rows          int
		wantRows      int
		wantTruncated bool
	}{
		{name: "under ceiling", rows: 3, wantRows: 3, wantTruncated: false},
		{name: "at ceiling", rows: relationships.RowLimit, wantRows: relationships.RowLimit, wantTruncated: false},
		{name: "over ceiling clips and signals", rows: relationships.FetchLimit, wantRows: relationships.RowLimit, wantTruncated: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := &codequery.CodeHandler{Neo4j: fakeGraphReader{
				run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
					if !strings.Contains(cypher, "MATCH (e:Function {uid: $entity_id})-[rel:CALLS]->(target)") {
						t.Fatalf("unexpected cypher (enrichment must be skipped for uid-less rows):\n%s", cypher)
					}
					if got, want := params["row_limit"], relationships.FetchLimit; got != want {
						t.Fatalf("row_limit = %#v, want over-fetch %d", got, want)
					}
					// Rows carry no *_entity_uid, so enrichment is skipped and the
					// fake never sees an enrichment query.
					rows := make([]map[string]any, 0, tt.rows)
					for i := 0; i < tt.rows; i++ {
						rows = append(rows, map[string]any{
							"direction": "outgoing",
							"type":      "CALLS",
							"target_id": "content-entity:callee-" + strconv.Itoa(i),
						})
					}
					return rows, nil
				},
			}}

			got, truncated, err := relationships.OneHopRelationships(
				context.Background(), handler.Neo4j, "content-entity:hub", "outgoing", "CALLS", "Function",
			)
			if err != nil {
				t.Fatalf("nornicDBOneHopRelationships() error = %v", err)
			}
			if len(got) != tt.wantRows {
				t.Fatalf("len(rows) = %d, want %d", len(got), tt.wantRows)
			}
			if truncated != tt.wantTruncated {
				t.Fatalf("truncated = %v, want %v", truncated, tt.wantTruncated)
			}
		})
	}
}

// TestCollectNornicDBEnrichmentPreservesFileWithoutRepository proves the split
// File/Repository enrichment keeps an endpoint's file metadata even when it has
// no REPO_CONTAINS edge, matching the pre-split OPTIONAL MATCH behavior (#5730
// review, Codex P2).
func TestCollectNornicDBEnrichmentPreservesFileWithoutRepository(t *testing.T) {
	t.Parallel()

	meta := make(map[string]relationships.EndpointMeta)
	// File read yields path+language for both endpoints.
	relationships.CollectEnrichment(meta, []map[string]any{
		{"entity_uid": "cls:HasRepo", "file_path": "a.py", "file_language": "python"},
		{"entity_uid": "cls:FileOnly", "file_path": "b.py", "file_language": "python"},
	})
	// Repository read yields repo fields only for the endpoint that has a repo.
	relationships.CollectEnrichment(meta, []map[string]any{
		{"entity_uid": "cls:HasRepo", "repo_id": "repo:1", "repo_name": "svc"},
	})

	hasRepo := meta["cls:HasRepo"]
	if hasRepo.FilePath != "a.py" || hasRepo.RepoID != "repo:1" || hasRepo.RepoName != "svc" {
		t.Fatalf("HasRepo meta = %+v, want full file+repo metadata", hasRepo)
	}
	fileOnly := meta["cls:FileOnly"]
	if fileOnly.FilePath != "b.py" || fileOnly.FileLanguage != "python" {
		t.Fatalf("FileOnly meta = %+v, want file path/language preserved", fileOnly)
	}
	if fileOnly.RepoID != "" || fileOnly.RepoName != "" {
		t.Fatalf("FileOnly meta = %+v, want no repository metadata fabricated", fileOnly)
	}
}
