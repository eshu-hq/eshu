// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// termsMaterializedCases pin which scoped one-term requests hide the term from
// the entity probe (#7246). Hiding the term gives the planner the default match
// selectivity, which is right for a term the trigram indexes can filter on and
// wrong for one they cannot: a pattern with no run of three letters or digits
// has no extractable trigram, so both GIN scans return every row. Measured on a
// 1M-row PostgreSQL 18.6 shim: 4.5 to 137 ms with the literal visible against
// 3.8 to 5.8 s hidden. Such a term must keep the plain CTE. In a LIKE pattern
// "_" and "%" are wildcards and break a word, as does any other character that
// is not an ASCII letter or digit. Only ASCII counts: under a C-ctype database
// PostgreSQL treats multibyte characters as non-alphanumeric for trigram
// extraction, so a purely non-ASCII term has none there; ops-qa runs en_US.UTF-8
// but the guard cannot assume it. "ab-cd" does have trigrams (pg_trgm pads a
// word next to punctuation); the guard is conservative for it and costs nothing,
// because the plain statement is the base.
var termsMaterializedCases = []struct {
	term string
	want bool
}{
	{"decode", true},
	{"abc", true},
	{"foo_bar", true},
	{"ab_cde", true},
	{"db_", false},
	{"pg_", false},
	{"a_b", false},
	{"a%b", false},
	{"ab-cd", false},
	{"__", false},
	{"字", false},
	{"字符串", false},
	{"данные", false},
	{"ab字cd", false},
	{"foo字", true},
	{"café", true},
	{"äöü", false},
	{"αβγ", false},
	{"über", true},
}

func TestInvestigateCodeTopicHidesOnlyTermsTheTrigramIndexCanFilter(t *testing.T) {
	t.Parallel()
	for _, tc := range termsMaterializedCases {
		t.Run(tc.term, func(t *testing.T) {
			t.Parallel()
			query := codeTopicSQLFor(t, CodeTopicInvestigationRequest{
				RepoID: "repo-1", Terms: []string{tc.term}, Limit: 13,
			})
			if got := strings.Contains(query, "terms(term) AS MATERIALIZED"); got != tc.want {
				t.Fatalf("term %q: MATERIALIZED terms CTE = %v, want %v", tc.term, got, tc.want)
			}
		})
	}
}

// An operator reading a trace must tell a scoped one-term request that hid the
// term from one that kept it visible: code_topic.terms_materialized.
func TestInvestigateCodeTopicSpanMarksMaterializedTerms(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		req  CodeTopicInvestigationRequest
		want bool
	}{
		{"hidden", CodeTopicInvestigationRequest{RepoID: "repo-1", Terms: []string{"decode"}, Limit: 13}, true},
		{"scoped_but_no_trigram", CodeTopicInvestigationRequest{RepoID: "repo-1", Terms: []string{"db_"}, Limit: 13}, false},
		{"unscoped", CodeTopicInvestigationRequest{Terms: []string{"decode"}, Limit: 13}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			recorder := tracetest.NewSpanRecorder()
			provider := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
			db, _ := openRecordingContentSearchDB(t, []contentSearchQueryResult{{
				columns: []string{
					"source_kind", "repo_id", "relative_path", "entity_id", "entity_name",
					"entity_type", "language", "start_line", "end_line", "matched_terms",
					"score", "pool_truncated",
				},
			}})
			reader := NewContentReader(db)
			reader.tracer = provider.Tracer("code-topic-terms-materialized-test")
			if _, err := reader.InvestigateCodeTopic(context.Background(), tc.req); err != nil {
				t.Fatalf("InvestigateCodeTopic() error = %v", err)
			}
			spans := recorder.Ended()
			if len(spans) != 1 {
				t.Fatalf("ended spans = %d, want 1", len(spans))
			}
			found, got := false, false
			for _, kv := range spans[0].Attributes() {
				if kv.Key == attribute.Key("code_topic.terms_materialized") {
					found, got = true, kv.Value.AsBool()
				}
			}
			if !found || got != tc.want {
				t.Fatalf("code_topic.terms_materialized found=%v value=%v, want %v", found, got, tc.want)
			}
		})
	}
}
