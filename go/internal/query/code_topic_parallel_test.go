// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codetopicparallel"
)

func TestInvestigateCodeTopicParallelFallsBackWhenPoolIsSmall(t *testing.T) {
	db, recorder := openRecordingContentSearchDB(t, []contentSearchQueryResult{{
		columns: []string{"source_kind", "repo_id", "relative_path", "entity_id", "entity_name", "entity_type", "language", "start_line", "end_line", "matched_terms", "score", "pool_truncated"},
		rows:    [][]driver.Value{{"file", "repo", "a.go", "", "", "", "go", int64(1), int64(2), "same", int64(1), false}},
	}})
	db.SetMaxOpenConns(3)
	terms := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15"}
	rows, err := newCodeTopicParallelTestReader(db).InvestigateCodeTopic(context.Background(), CodeTopicInvestigationRequest{Terms: terms, Limit: 26})
	if err != nil || len(rows) != 1 {
		t.Fatalf("serial fallback rows=%#v err=%v", rows, err)
	}
	if len(recorder.queries) != 1 || !strings.Contains(recorder.queries[0], "WITH terms(term) AS") {
		t.Fatalf("serial fallback queries=%#v", recorder.queries)
	}
}

func TestInvestigateCodeTopicParallelFallsBackWithoutSnapshotSet(t *testing.T) {
	db, recorder := openRecordingContentSearchDB(t, []contentSearchQueryResult{{
		columns: []string{"source_kind", "repo_id", "relative_path", "entity_id", "entity_name", "entity_type", "language", "start_line", "end_line", "matched_terms", "score", "pool_truncated"},
		rows:    [][]driver.Value{{"file", "repo", "a.go", "", "", "", "go", int64(1), int64(2), "same", int64(1), false}},
	}})
	db.SetMaxOpenConns(4)
	terms := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15"}
	rows, err := NewContentReader(db).InvestigateCodeTopic(context.Background(), CodeTopicInvestigationRequest{Terms: terms, Limit: 26})
	if err != nil || len(rows) != 1 {
		t.Fatalf("serial fallback rows=%#v err=%v", rows, err)
	}
	if len(recorder.queries) != 1 || !strings.Contains(recorder.queries[0], "WITH terms(term) AS") {
		t.Fatalf("serial fallback queries=%#v", recorder.queries)
	}
}

func TestCodeTopicParallelSQLKeepsPostgresAssemblyContract(t *testing.T) {
	query := codetopicparallel.AssemblySQL(250)
	for _, fragment := range []string{
		"jsonb_to_recordset($1::jsonb)",
		"string_agg(DISTINCT matched_term, E'\\x1f' ORDER BY matched_term)",
		"count(DISTINCT matched_term)::int AS score",
		"GROUP BY source_kind, matched_term",
		"term_count >= 250",
		"ORDER BY score DESC, repo_id, relative_path, entity_name, source_kind",
		"LIMIT $2 OFFSET $3",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("assembly SQL missing %q", fragment)
		}
	}
	if got := strings.Count(query, "string_agg(DISTINCT matched_term"); got != 2 {
		t.Fatalf("aggregates = %d, want entity and file", got)
	}
	req := CodeTopicInvestigationRequest{
		AllowedRepositoryIDs: []string{"repo-1"}, Language: "go",
		Terms: []string{"a_c", "a%c", `back\slash`, "duplicate"},
	}
	filters, baseArgs, _ := codeTopicFilters(req)
	probe, args := codetopicparallel.ProbeSQL(req, 250, filters, baseArgs)
	for _, fragment := range []string{
		"path_pool AS MATERIALIZED", "LIMIT (SELECT 250 - count(*) FROM path_pool)",
		"f.relative_path NOT ILIKE", "repo_id = ANY($1)", "coalesce(language, '') = $2",
		"eshu_require_content_substring_indexes_ready()",
	} {
		if !strings.Contains(probe, fragment) {
			t.Fatalf("probe SQL missing %q", fragment)
		}
	}
	if len(args) != 6 || args[2] != "a_c" || args[3] != "a%c" || args[4] != `back\slash` {
		t.Fatalf("bound probe args = %#v", args)
	}
}

func TestCodeTopicParallelGateBoundsPoolUse(t *testing.T) {
	for _, tc := range []struct {
		terms, maxOpen int
		want           bool
	}{
		{15, 0, false}, {16, 0, false}, {16, 3, false}, {16, 4, true}, {17, 4, false},
	} {
		if got := codetopicparallel.Eligible(tc.terms, tc.maxOpen); got != tc.want {
			t.Errorf("gate(%d, %d) = %t, want %t", tc.terms, tc.maxOpen, got, tc.want)
		}
	}
}

func TestRunCodeTopicPartitionsCancelsAndJoinsOnFailure(t *testing.T) {
	failure := errors.New("probe failed")
	var active atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := codetopicparallel.RunPartitions(ctx, 4, func(ctx context.Context, index int) ([]codetopicparallel.ProbeRow, error) {
		active.Add(1)
		defer active.Add(-1)
		if index == 0 {
			return nil, failure
		}
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if !errors.Is(err, failure) {
		t.Fatalf("error = %v, want probe failure", err)
	}
	if got := active.Load(); got != 0 {
		t.Fatalf("active workers after return = %d, want 0", got)
	}
}
