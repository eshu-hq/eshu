// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"fmt"
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
		RepoID:               "repo-1",
		AllowedRepositoryIDs: []string{"repo-1"},
		Terms:                []string{"repo", "sync", "auth", "github"},
		Limit:                26,
		Offset:               0,
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
	if got, want := strings.Count(recorder.queries[0], "term_param AS MATERIALIZED"), 4; got != want {
		t.Fatalf("explicit repo term CTEs = %d, want %d even with a grant list", got, want)
	}
	if got, want := strings.Count(recorder.queries[0], "f.content ILIKE '%' || (SELECT term FROM term_param) || '%'"), 4; got != want {
		t.Fatalf("explicit repo content predicates = %d, want %d", got, want)
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
	// file_probe (content_files has no substring index on relative_path, so it
	// cannot share entity_probe's LATERAL form -- #7008 measured the LATERAL
	// shape forcing every term's content Seq Scan to run serially with no
	// parallel workers) must be a UNION of one independently bounded,
	// top-level per-term SELECT branch, not a per-term LATERAL probe.
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
	// #7033: each of the four term pools materializes its path matches before
	// evaluating a content-only probe for the remaining capacity. The final
	// union has one separator per term plus three between terms.
	if got, want := strings.Count(fileProbe, "UNION ALL"), 7; got != want {
		t.Fatalf("file_probe UNION ALL count = %d, want %d (path/content pools plus term boundaries)", got, want)
	}
	// A materialized path pool establishes the exact capacity before the content
	// branch starts. The content pool's count-gated limit is necessary because
	// UNION ALL plus a later outer LIMIT does not require PostgreSQL to emit the
	// path branch first.
	if got, want := strings.Count(fileProbe, "path_pool AS MATERIALIZED"), 4; got != want {
		t.Fatalf("path pool count = %d, want %d (one materialized path pool per term)", got, want)
	}
	if got, want := strings.Count(fileProbe, "LIMIT (SELECT 1000 - count(*) FROM path_pool)"), 4; got != want {
		t.Fatalf("content remaining-capacity limit count = %d, want %d (one per term)", got, want)
	}
	if strings.Contains(fileProbe, ") path_first\n\t\t  LIMIT") {
		t.Fatalf("file_probe = %q, want no unordered outer path_first LIMIT", fileProbe)
	}
	if got, want := strings.Count(fileProbe, "LIMIT 1000"), 4; got != want {
		t.Fatalf("file_probe path LIMIT 1000 count = %d, want %d (one path cap per term)", got, want)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if !rows[0].PoolTruncated {
		t.Fatalf("rows[0].PoolTruncated = false, want true when the backend reports a capped pool")
	}
}

// TestContentReaderInvestigateCodeTopicFileProbePrioritizesPaths proves #7033's
// path-first file candidate partition. It must retain the complete ILIKE match
// set when a term is uncapped, while a capped term may select a different
// bounded candidate pool. The path and content probes deliberately use the
// same term placeholder so wildcard and backslash semantics remain Postgres
// ILIKE semantics rather than a lossy normalized-string approximation.
func TestContentReaderInvestigateCodeTopicFileProbePrioritizesPaths(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentSearchDB(t, []contentSearchQueryResult{{
		columns: []string{
			"source_kind", "repo_id", "relative_path", "entity_id", "entity_name",
			"entity_type", "language", "start_line", "end_line", "matched_terms", "score",
			"pool_truncated",
		},
		rows: [][]driver.Value{{
			"file", "repo-1", "internal/a_c.go", "", "", "", "go", int64(1), int64(1),
			"a_c", int64(1), true,
		}},
	}})
	reader := NewContentReader(db)

	terms := []string{"path-only", "content-only", "both", "a_c", "a%c", `back\slash`}
	rows, err := reader.InvestigateCodeTopic(context.Background(), CodeTopicInvestigationRequest{
		AllowedRepositoryIDs: []string{"repo-1"},
		Language:             "go",
		Terms:                terms,
		Limit:                26,
	})
	if err != nil {
		t.Fatalf("InvestigateCodeTopic() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if !rows[0].PoolTruncated {
		t.Fatal("PoolTruncated = false, want capped file-pool marker preserved")
	}

	query := recorder.queries[0]
	fileProbeStart := strings.Index(query, "file_probe AS (")
	fileMatchesStart := strings.Index(query, "file_matches AS (")
	if fileProbeStart == -1 || fileMatchesStart == -1 || fileMatchesStart < fileProbeStart {
		t.Fatalf("query = %q, want a file_probe CTE before file_matches", query)
	}
	fileProbe := query[fileProbeStart:fileMatchesStart]
	if got, want := strings.Count(fileProbe, "f.relative_path ILIKE"), len(terms); got != want {
		t.Fatalf("path predicate count = %d, want %d (one path probe per term)", got, want)
	}
	if got, want := strings.Count(fileProbe, "f.content ILIKE"), len(terms); got != want {
		t.Fatalf("content predicate count = %d, want %d (one content-only probe per term)", got, want)
	}
	if got := strings.Count(fileProbe, "term_param AS MATERIALIZED"); got != 0 {
		t.Fatalf("grant-list search term CTEs = %d, want none", got)
	}
	if got, want := strings.Count(fileProbe, "f.content ILIKE '%' || $"), len(terms); got != want {
		t.Fatalf("grant-list direct content-term predicates = %d, want %d", got, want)
	}
	if got, want := strings.Count(fileProbe, "f.relative_path NOT ILIKE"), len(terms); got != want {
		t.Fatalf("path exclusion count = %d, want %d (content probe must exclude path hits)", got, want)
	}

	// Six terms make the fixed 4,000-row pool budget a 666-row per-term cap.
	// A materialized path pool makes the remaining content capacity explicit.
	// When the path pool fills, PostgreSQL can satisfy the zero-row content
	// limit without scanning that branch; when it does not, the two pools retain
	// the full path-or-content match set without duplicate path/content hits.
	if got, want := strings.Count(fileProbe, "path_pool AS MATERIALIZED"), len(terms); got != want {
		t.Fatalf("path pool count = %d, want %d (one materialized path pool per term)", got, want)
	}
	if got, want := strings.Count(fileProbe, "content_pool AS ("), len(terms); got != want {
		t.Fatalf("content pool count = %d, want %d (one count-gated content pool per term)", got, want)
	}
	if got, want := strings.Count(fileProbe, "LIMIT (SELECT 666 - count(*) FROM path_pool)"), len(terms); got != want {
		t.Fatalf("remaining-capacity limit count = %d, want %d (one per term)", got, want)
	}
	if strings.Contains(fileProbe, ") path_first\n\t\t  LIMIT") {
		t.Fatalf("file_probe = %q, want no unordered outer path_first LIMIT", fileProbe)
	}
	if got, want := strings.Count(fileProbe, "LIMIT 666"), len(terms); got != want {
		t.Fatalf("file_probe path LIMIT count = %d, want %d (one path cap per term)", got, want)
	}
	if got, want := strings.Count(fileProbe, "repo_id = ANY($1)"), len(terms)*2; got != want {
		t.Fatalf("repo filter count = %d, want %d (both file subprobes per term)", got, want)
	}
	if got, want := strings.Count(fileProbe, "coalesce(language, '') = $2"), len(terms)*2; got != want {
		t.Fatalf("language filter count = %d, want %d (both file subprobes per term)", got, want)
	}
	if got, want := strings.Count(fileProbe, "eshu_require_content_substring_indexes_ready()"), len(terms)*2; got != want {
		t.Fatalf("readiness gate count = %d, want %d (both file subprobes per term)", got, want)
	}

	// Terms start at $3 after repo and language. Each one is the matched-term
	// value in both subprobes plus path, content, and path-exclusion predicates:
	// five uses of the exact bound value. In particular, underscores, percent signs, and
	// backslashes stay ILIKE input rather than being escaped or rewritten.
	for i, want := range terms {
		argIndex := i + 3
		if got := recorder.args[0][argIndex-1]; got != want {
			t.Fatalf("term arg $%d = %#v, want %#v", argIndex, got, want)
		}
		placeholder := fmt.Sprintf("$%d", argIndex)
		if got, wantUses := strings.Count(fileProbe, placeholder), 5; got != wantUses {
			t.Fatalf("term placeholder %s uses = %d, want %d (matched values plus path/content/exclusion)", placeholder, got, wantUses)
		}
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
		RepoID: " \t",
		Terms:  []string{"auth"},
		Limit:  26,
	})
	if err != nil {
		t.Fatalf("InvestigateCodeTopic() error = %v, want nil", err)
	}
	if !strings.Contains(recorder.queries[0], "eshu_require_content_substring_indexes_ready()") {
		t.Fatalf("query = %q, want durable unscoped substring-index readiness gate", recorder.queries[0])
	}
	if strings.Contains(recorder.queries[0], "term_param AS MATERIALIZED") {
		t.Fatal("unscoped query must retain its original file content predicate")
	}
	if !strings.Contains(recorder.queries[0], "f.content ILIKE '%' || $1 || '%'") {
		t.Fatal("unscoped query must use the original bound content term")
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
