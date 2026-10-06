// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// codeTopicSQLFor runs InvestigateCodeTopic against a recording driver and
// returns the single statement it issued.
func codeTopicSQLFor(t *testing.T, req CodeTopicInvestigationRequest) string {
	t.Helper()
	db, recorder := openRecordingContentSearchDB(t, []contentSearchQueryResult{{
		columns: []string{
			"source_kind", "repo_id", "relative_path", "entity_id", "entity_name",
			"entity_type", "language", "start_line", "end_line", "matched_terms",
			"score", "pool_truncated",
		},
	}})
	if _, err := NewContentReader(db).InvestigateCodeTopic(context.Background(), req); err != nil {
		t.Fatalf("InvestigateCodeTopic() error = %v", err)
	}
	if len(recorder.queries) != 1 {
		t.Fatalf("statements = %d, want 1", len(recorder.queries))
	}
	return recorder.queries[0]
}

// entityProbeSection returns the entity_probe CTE text.
func entityProbeSection(t *testing.T, query string) string {
	t.Helper()
	start := strings.Index(query, "entity_probe AS (")
	end := strings.Index(query, "entity_matches AS (")
	if start == -1 || end == -1 || end < start {
		t.Fatalf("query has no entity_probe CTE before entity_matches: %q", query)
	}
	return query[start:end]
}

// #7246: the scoped one-term statement runs as a custom plan, so a plain
// terms VALUES list hands the literal to the planner. For a corpus-common term
// the trigram estimate is far too low and the planner skips the repo bitmap.
// The terms CTE must be MATERIALIZED so the entity probe plans with the
// parameter hidden, and the entity probe must read it from that CTE.
func TestInvestigateCodeTopicScopedOneTermHidesTermFromEntityProbe(t *testing.T) {
	t.Parallel()
	query := codeTopicSQLFor(t, CodeTopicInvestigationRequest{
		RepoID: "repo-1", Terms: []string{"decode"}, Limit: 13,
	})
	if got := strings.Count(query, "terms(term) AS MATERIALIZED"); got != 1 {
		t.Fatalf("MATERIALIZED terms CTEs = %d, want 1", got)
	}
	if !strings.Contains(query, "terms(term) AS MATERIALIZED (\n\t\t  VALUES ($2)\n\t\t)") {
		t.Fatalf("terms CTE does not materialize the single bound term: %q", query)
	}
	probe := entityProbeSection(t, query)
	for _, want := range []string{
		"FROM terms\n",
		"e.entity_name ILIKE '%' || terms.term || '%'",
		"e.source_cache ILIKE '%' || terms.term || '%'",
	} {
		if !strings.Contains(probe, want) {
			t.Fatalf("entity_probe missing %q: %s", want, probe)
		}
	}
	if strings.Contains(probe, "ILIKE '%' || $") {
		t.Fatalf("entity_probe binds the term parameter directly: %s", probe)
	}
}

// #7246: golden SQL pins every statement shape InvestigateCodeTopic builds on
// the single-statement path. Five shapes stay byte-identical to the SQL on
// origin/main 9bcca588f (goldens captured there); the scoped one-term shape
// differs from it only by the word MATERIALIZED on the terms CTE. Goldens are
// stored without trailing whitespace; the comparison trims the statement tail
// (the builder ends the text with a newline and a tab).
func TestInvestigateCodeTopicStatementShapesMatchGolden(t *testing.T) {
	t.Parallel()
	sixteen := make([]string, 16)
	for i := range sixteen {
		sixteen[i] = fmt.Sprint(i)
	}
	cases := []struct {
		name         string
		req          CodeTopicInvestigationRequest
		materialized bool
	}{
		{"scoped_one_term", CodeTopicInvestigationRequest{RepoID: "repo-1", Terms: []string{"decode"}, Limit: 13}, true},
		{"unscoped_one_term", CodeTopicInvestigationRequest{Terms: []string{"decode"}, Limit: 13}, false},
		{"scoped_three_terms", CodeTopicInvestigationRequest{RepoID: "repo-1", Terms: []string{"repo", "sync", "auth"}, Limit: 26}, false},
		{"scoped_sixteen_terms_serial", CodeTopicInvestigationRequest{RepoID: "repo-1", Terms: sixteen, Limit: 26}, false},
		{"scoped_one_term_language", CodeTopicInvestigationRequest{RepoID: "repo-1", Language: "go", Terms: []string{"decode"}, Limit: 13}, false},
		{"grant_list_one_term", CodeTopicInvestigationRequest{AllowedRepositoryIDs: []string{"repo-1", "repo-2"}, Terms: []string{"decode"}, Limit: 13}, false},
	}
	const trim = " \t\n"
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			query := codeTopicSQLFor(t, tc.req)
			path := filepath.Join("testdata", "code_topic_sql", tc.name+".sql")
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			if strings.TrimRight(query, trim) != strings.TrimRight(string(want), trim) {
				t.Fatalf("%s SQL changed from golden %s", tc.name, path)
			}
			if got := strings.Contains(query, "terms(term) AS MATERIALIZED"); got != tc.materialized {
				t.Fatalf("%s MATERIALIZED terms CTE = %v, want %v", tc.name, got, tc.materialized)
			}
		})
	}
}
