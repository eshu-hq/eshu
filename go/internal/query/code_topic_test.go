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
	if !strings.Contains(recorder.queries[0], "WITH entity_probe AS") {
		t.Fatalf("query = %q, want scored entity probe CTE", recorder.queries[0])
	}
	// repo_id ($1), then one bound arg per term ($2-$5, in request order),
	// then limit/offset (#7033: each term is its own placeholder in matching
	// entity and file UNION branches, instead of one delimited-string arg
	// unnested in SQL).
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
// independently bounded per-term UNION ALL branches, instead of the unbounded
// `JOIN terms ON <or condition>` that let an unscoped topic search
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
	// codeTopicCandidatePoolBudget (4000) / 4 terms = 1000, above the floor.
	if !strings.Contains(query, "LIMIT 1000") {
		t.Fatalf("query = %q, want per-term candidate cap 1000 for 4 terms", query)
	}
	if strings.Contains(query, "JOIN terms ON") {
		t.Fatalf("query = %q, want no unbounded JOIN...ON match probe", query)
	}
	// Both probes must be static top-level UNION branches. The entity form used
	// to be a per-term LATERAL probe; with a large unscoped corpus that shape
	// performed a full entity scan for each term before it reached the indexed
	// ILIKE predicates. Each branch must retain the OR predicate intact so a
	// source_cache-only match remains eligible.
	entityProbeStart := strings.Index(query, "entity_probe AS (")
	entityMatchesStart := strings.Index(query, "entity_matches AS (")
	if entityProbeStart == -1 || entityMatchesStart == -1 || entityMatchesStart < entityProbeStart {
		t.Fatalf("query = %q, want an entity_probe CTE before entity_matches", query)
	}
	entityProbe := query[entityProbeStart:entityMatchesStart]
	if strings.Contains(entityProbe, "LATERAL") {
		t.Fatalf("entity_probe = %q, want no per-term LATERAL form", entityProbe)
	}
	if got, want := strings.Count(entityProbe, "FROM content_entities e"), 4; got != want {
		t.Fatalf("entity_probe entity branch count = %d, want %d", got, want)
	}
	if got, want := strings.Count(entityProbe, "UNION ALL"), 3; got != want {
		t.Fatalf("entity_probe UNION ALL count = %d, want %d (one join per term boundary)", got, want)
	}
	if got, want := strings.Count(entityProbe, "LIMIT 1000"), 4; got != want {
		t.Fatalf("entity_probe LIMIT 1000 count = %d, want %d (one per-term branch cap)", got, want)
	}
	if got, want := strings.Count(entityProbe, "ORDER BY e.entity_id"), 4; got != want {
		t.Fatalf("entity_probe deterministic order count = %d, want %d", got, want)
	}
	if got, want := strings.Count(entityProbe, "e.source_cache ILIKE"), 4; got != want {
		t.Fatalf("entity_probe source-cache OR arm count = %d, want %d", got, want)
	}
	// file_probe uses the same static shape. relative_path has its own
	// substring index after #7033, so each branch can use that predicate before
	// its cap without serial LATERAL scans.
	fileProbeStart := strings.Index(query, "file_probe AS (")
	fileMatchesStart := strings.Index(query, "file_matches AS (")
	if fileProbeStart == -1 || fileMatchesStart == -1 || fileMatchesStart < fileProbeStart {
		t.Fatalf("query = %q, want a file_probe CTE before file_matches", query)
	}
	fileProbe := query[fileProbeStart:fileMatchesStart]
	if strings.Contains(fileProbe, "LATERAL") {
		t.Fatalf("file_probe = %q, want no per-term LATERAL form", fileProbe)
	}
	if !strings.Contains(fileProbe, "FROM content_files f") {
		t.Fatalf("file_probe = %q, want a content_files probe", fileProbe)
	}
	// 4 terms join into 4 file branches with 3 UNION ALL separators.
	if got, want := strings.Count(fileProbe, "UNION ALL"), 3; got != want {
		t.Fatalf("file_probe UNION ALL count = %d, want %d (one join per term boundary)", got, want)
	}
	// Each of the 4 branches carries its own LIMIT candidateCap, not just the
	// entity_probe side: dropping it per-branch leaves content_files unbounded
	// again with every other assertion in this test still passing.
	if got, want := strings.Count(fileProbe, "LIMIT 1000"), 4; got != want {
		t.Fatalf("file_probe LIMIT 1000 count = %d, want %d (one per-term branch cap)", got, want)
	}
	if got, want := strings.Count(fileProbe, "ORDER BY f.repo_id, f.relative_path"), 4; got != want {
		t.Fatalf("file_probe deterministic order count = %d, want %d", got, want)
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

// TestContentReaderInvestigateCodeTopicEmptyTermsReturnsNothing proves the
// empty-Terms guard: no candidate cap, filter, or query can be built without
// at least one term, so InvestigateCodeTopic must return before issuing any
// SQL rather than run a term-less probe.
func TestContentReaderInvestigateCodeTopicEmptyTermsReturnsNothing(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentSearchDB(t, nil)
	reader := NewContentReader(db)

	rows, err := reader.InvestigateCodeTopic(context.Background(), CodeTopicInvestigationRequest{
		RepoID: "repo-1",
		Terms:  nil,
		Limit:  26,
		Offset: 0,
	})
	if err != nil {
		t.Fatalf("InvestigateCodeTopic() error = %v, want nil", err)
	}
	if rows != nil {
		t.Fatalf("rows = %#v, want nil for empty Terms", rows)
	}
	if got, want := len(recorder.queries), 0; got != want {
		t.Fatalf("queries = %d, want %d (no SQL issued for empty Terms)", got, want)
	}
}
