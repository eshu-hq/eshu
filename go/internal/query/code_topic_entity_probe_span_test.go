// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// #7246: an operator reading a slow code-topic trace must be able to tell
// whether the statement ran the scoped one-term shape (MATERIALIZED terms CTE,
// custom plan) without reading the SQL. The postgres.query span carries
// code_topic.scoped_one_term for every single-statement run.
func TestInvestigateCodeTopicSpanMarksScopedOneTermShape(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		req  CodeTopicInvestigationRequest
		want bool
	}{
		{name: "scoped_one_term", req: CodeTopicInvestigationRequest{RepoID: "repo-1", Terms: []string{"decode"}, Limit: 13}, want: true},
		{name: "unscoped_one_term", req: CodeTopicInvestigationRequest{Terms: []string{"decode"}, Limit: 13}, want: false},
		{name: "scoped_one_term_language", req: CodeTopicInvestigationRequest{RepoID: "repo-1", Language: "go", Terms: []string{"decode"}, Limit: 13}, want: false},
		{name: "scoped_three_terms", req: CodeTopicInvestigationRequest{RepoID: "repo-1", Terms: []string{"a", "b", "c"}, Limit: 13}, want: false},
	}
	for _, tc := range cases {
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
			reader.tracer = provider.Tracer("code-topic-entity-probe-span-test")
			if _, err := reader.InvestigateCodeTopic(context.Background(), tc.req); err != nil {
				t.Fatalf("InvestigateCodeTopic() error = %v", err)
			}

			spans := recorder.Ended()
			if len(spans) != 1 {
				t.Fatalf("ended spans = %d, want 1", len(spans))
			}
			var got, found bool
			for _, kv := range spans[0].Attributes() {
				if kv.Key == attribute.Key("code_topic.scoped_one_term") {
					got, found = kv.Value.AsBool(), true
				}
			}
			if !found {
				t.Fatalf("span has no code_topic.scoped_one_term attribute: %v", spans[0].Attributes())
			}
			if got != tc.want {
				t.Fatalf("code_topic.scoped_one_term = %v, want %v", got, tc.want)
			}
		})
	}
}
