// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// cappedListStore returns min(count, limit) synthetic rows, like a LIMIT-bound
// Postgres read, and records the limits it was asked for.
type cappedListStore struct {
	querycontract.ContentStore
	entities, files                int
	entityLimitSeen, fileLimitSeen int
}

func (s *cappedListStore) ListRepoEntities(_ context.Context, repoID string, limit int) ([]querycontract.EntityContent, error) {
	s.entityLimitSeen = limit
	rows := make([]querycontract.EntityContent, 0, min(limit, s.entities))
	for i := 0; i < s.entities && i < limit; i++ {
		rows = append(rows, querycontract.EntityContent{EntityID: fmt.Sprint(i), RepoID: repoID, EntityType: "Function", EntityName: "handler", Language: "python", Metadata: map[string]any{"decorators": []any{"@route"}, "async": true}})
	}
	return rows, nil
}

func (s *cappedListStore) ListRepoFiles(_ context.Context, repoID string, limit int) ([]querycontract.FileContent, error) {
	s.fileLimitSeen = limit
	rows := make([]querycontract.FileContent, 0, min(limit, s.files))
	for i := 0; i < s.files && i < limit; i++ {
		rows = append(rows, querycontract.FileContent{RepoID: repoID, RelativePath: fmt.Sprintf("f%d.go", i)})
	}
	return rows, nil
}

// TestLoadRepositorySemanticOverviewReportsTruncationAtCap pins the #7126
// sentinel contract at cap-1, cap, and cap+1 for each list: the read asks for
// cap+1 rows, the returned file list never exceeds the cap (downstream stages
// see the rows they always did), and truncated is true only when the sentinel
// row past the cap exists.
func TestLoadRepositorySemanticOverviewReportsTruncationAtCap(t *testing.T) {
	t.Parallel()

	const limit = querycontract.RepositorySemanticEntityLimit
	for _, tc := range []struct {
		name            string
		entities, files int
		wantTruncated   bool
		wantFiles       int
	}{
		{"cap-1 both", limit - 1, limit - 1, false, limit - 1},
		{"cap both", limit, limit, false, limit},
		{"entities cap+1", limit + 1, 1, true, 1},
		{"files cap+1", 1, limit + 1, true, limit},
		{"empty", 0, 0, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := &cappedListStore{entities: tc.entities, files: tc.files}
			overview, files, truncated, err := loadRepositorySemanticOverview(context.Background(), store, "repo-1")
			if err != nil {
				t.Fatalf("loadRepositorySemanticOverview() error = %v, want nil", err)
			}
			if truncated != tc.wantTruncated {
				t.Fatalf("truncated = %v, want %v", truncated, tc.wantTruncated)
			}
			if len(files) != tc.wantFiles {
				t.Fatalf("len(files) = %d, want %d (never more than the cap)", len(files), tc.wantFiles)
			}
			if store.entityLimitSeen != limit+1 || store.fileLimitSeen != limit+1 {
				t.Fatalf("read limits = entities %d, files %d, want %d (cap plus sentinel)", store.entityLimitSeen, store.fileLimitSeen, limit+1)
			}
			if tc.entities > 0 {
				counts, _ := overview["entity_type_counts"].(map[string]int)
				if got, want := counts["Function"], min(tc.entities, limit); got != want {
					t.Fatalf("overview counted %d entities, want %d (clipped to the cap)", got, want)
				}
			}
		})
	}
}

// TestSemanticReadTruncatedReasonNamesTheLimit pins the wire-visible reason to
// the cap it reports, so changing RepositorySemanticEntityLimit without the
// reason (or the reverse) fails here instead of shipping a reason that lies.
func TestSemanticReadTruncatedReasonNamesTheLimit(t *testing.T) {
	t.Parallel()
	want := fmt.Sprintf("repository_semantic_read_truncated_at_%d", querycontract.RepositorySemanticEntityLimit)
	if SemanticReadTruncatedReason != want {
		t.Fatalf("SemanticReadTruncatedReason = %q, want %q (drifted from RepositorySemanticEntityLimit)", SemanticReadTruncatedReason, want)
	}
}
