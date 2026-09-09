// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
)

// Code-topic ContentReader proofs that live in package query: they drive
// root's ContentReader SQL builders directly, which codequery cannot name
// without importing the root back (#6060). Split from
// codequery/topic_test.go at the lane-A move; the handler-level topic
// proofs stay there.

func TestContentReaderInvestigateCodeTopicUsesOneScoredQuery(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentSearchDB(t, []contentSearchQueryResult{
		{
			columns: []string{
				"source_kind", "repo_id", "relative_path", "entity_id", "entity_name",
				"entity_type", "language", "start_line", "end_line", "matched_terms", "score",
			},
			rows: [][]driver.Value{
				{
					"entity", "repo-1", "go/internal/collector/reposync/auth.go", "entity-auth",
					"resolveGitHubAppAuth", "Function", "go", int64(44), int64(88),
					"auth\x1fgithub\x1frepo\x1fsync", int64(4),
				},
			},
		},
	})
	reader := NewContentReader(db)

	rows, err := reader.InvestigateCodeTopic(context.Background(), CodeTopicInvestigationRequest{
		RepoID: "repo-1",
		Terms:  []string{"repo", "sync", "auth", "github"},
		Limit:  26,
		Offset: 0,
	})
	if err != nil {
		t.Fatalf("InvestigateCodeTopic() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got, want := len(recorder.queries), 1; got != want {
		t.Fatalf("queries = %d, want one scored SQL query", got)
	}
	if !strings.Contains(recorder.queries[0], "WITH terms AS") {
		t.Fatalf("query = %q, want scored terms CTE", recorder.queries[0])
	}
	if got, want := recorder.args[0][0], "repo-1"; got != want {
		t.Fatalf("repo arg = %#v, want %#v", got, want)
	}
	if got, want := recorder.args[0][1], "repo\x1fsync\x1fauth\x1fgithub"; got != want {
		t.Fatalf("terms arg = %#v, want %#v", got, want)
	}
	if strings.Contains(recorder.queries[0], "eshu_require_content_substring_indexes_ready()") {
		t.Fatalf("repo-scoped query = %q, must remain available during global index finalization", recorder.queries[0])
	}
}

func TestInvestigateCodeTopicUnscopedRequiresSubstringIndexesReady(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{{
		columns: []string{
			"source_kind", "repo_id", "relative_path", "entity_id", "entity_name",
			"entity_type", "language", "start_line", "end_line", "matched_terms", "score",
		},
	}})
	reader := NewContentReader(db)

	_, err := reader.InvestigateCodeTopic(context.Background(), CodeTopicInvestigationRequest{
		Terms: []string{"auth"},
		Limit: 26,
	})
	if err != nil {
		t.Fatalf("InvestigateCodeTopic() error = %v, want nil", err)
	}
	if !strings.Contains(recorder.queries[0], "eshu_require_content_substring_indexes_ready()") {
		t.Fatalf("query = %q, want durable unscoped substring-index readiness gate", recorder.queries[0])
	}
}
