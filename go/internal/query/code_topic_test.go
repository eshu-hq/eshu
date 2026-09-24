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
				"pool_truncated",
			},
			rows: [][]driver.Value{
				{
					"entity", "repo-1", "go/internal/collector/reposync/auth.go", "entity-auth",
					"resolveGitHubAppAuth", "Function", "go", int64(44), int64(88),
					"auth\x1fgithub\x1frepo\x1fsync", int64(4), false,
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
	if !strings.Contains(recorder.queries[0], "WITH terms(term) AS") {
		t.Fatalf("query = %q, want scored terms CTE", recorder.queries[0])
	}
	// repo_id ($1), then one bound arg per term ($2-$5, in request order),
	// then limit/offset (#7008: each term is its own placeholder now, shared
	// by entity_probe's LATERAL terms table and file_probe's per-term UNION
	// branches, instead of one delimited-string arg unnested in SQL).
	if got, want := len(recorder.args[0]), 7; got != want {
		t.Fatalf("len(query args) = %d, want %d (repo_id + 4 terms + limit + offset)", got, want)
	}
	if got, want := recorder.args[0][0], "repo-1"; got != want {
		t.Fatalf("repo arg = %#v, want %#v", got, want)
	}
	for i, want := range []string{"repo", "sync", "auth", "github"} {
		if got := recorder.args[0][1+i]; got != want {
			t.Fatalf("term arg[%d] = %#v, want %#v", i, got, want)
		}
	}
	if strings.Contains(recorder.queries[0], "eshu_require_content_substring_indexes_ready()") {
		t.Fatalf("repo-scoped query = %q, must remain available during global index finalization", recorder.queries[0])
	}
}

// TestContentReaderInvestigateCodeTopicBoundsCandidatePoolPerTerm proves
// #7008's fan-out bound: each per-term match probe runs inside a bounded
// `CROSS JOIN LATERAL (... LIMIT n)` scaled by term count, instead of the
// unbounded `JOIN terms ON <or condition>` that let an unscoped topic search
// materialize and sort every match in content_entities/content_files before
// applying the page LIMIT (measured to exceed a 60s statement_timeout at
// corpus scale on a live read replica).
func TestContentReaderInvestigateCodeTopicBoundsCandidatePoolPerTerm(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentSearchDB(t, []contentSearchQueryResult{
		{
			columns: []string{
				"source_kind", "repo_id", "relative_path", "entity_id", "entity_name",
				"entity_type", "language", "start_line", "end_line", "matched_terms", "score",
				"pool_truncated",
			},
			rows: [][]driver.Value{
				{
					"entity", "repo-1", "go/internal/collector/reposync/auth.go", "entity-auth",
					"resolveGitHubAppAuth", "Function", "go", int64(44), int64(88),
					"auth", int64(1), true,
				},
			},
		},
	})
	reader := NewContentReader(db)

	rows, err := reader.InvestigateCodeTopic(context.Background(), CodeTopicInvestigationRequest{
		Terms:  []string{"repo", "sync", "auth", "github"},
		Limit:  26,
		Offset: 0,
	})
	if err != nil {
		t.Fatalf("InvestigateCodeTopic() error = %v, want nil", err)
	}
	if got, want := len(recorder.queries), 1; got != want {
		t.Fatalf("queries = %d, want one scored SQL query", got)
	}
	query := recorder.queries[0]
	if !strings.Contains(query, "CROSS JOIN LATERAL") {
		t.Fatalf("query = %q, want bounded LATERAL match probe", query)
	}
	// codeTopicCandidatePoolBudget (4000) / 4 terms = 1000, above the floor.
	if !strings.Contains(query, "LIMIT 1000") {
		t.Fatalf("query = %q, want per-term candidate cap 1000 for 4 terms", query)
	}
	if strings.Contains(query, "JOIN terms ON") {
		t.Fatalf("query = %q, want no unbounded JOIN...ON match probe", query)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if !rows[0].PoolTruncated {
		t.Fatalf("rows[0].PoolTruncated = false, want true when the backend reports a capped pool")
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
