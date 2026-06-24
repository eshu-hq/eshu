// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestListActiveJVMReachabilityFactsQueryIsRepositoryAPIPackageAndLanguageBounded(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"WITH api_packages AS (",
		"LOWER(BTRIM(api_package)) AS api_package",
		"UNNEST($2::text[]) AS api_package",
		"fact.payload->>'repo_id' = ANY($1::text[])",
		"LOWER(COALESCE(fact.payload->'parsed_file_data'->>'lang', '')) IN ('java', 'kotlin', 'scala')",
		"jsonb_array_elements(",
		"fact.payload->'parsed_file_data'->'imports'",
		"fact.payload->'parsed_file_data'->'function_calls'",
		"fact.payload->'parsed_file_data'->'function_calls_scip'",
		"jvm_reachability_value",
		"LIKE api_package || '.%'",
		"LIKE '%.' || api_package || '.%'",
		"fact.observed_at, fact.fact_id) > ($3::timestamptz, $4::text)",
		"LIMIT $5",
	} {
		if !strings.Contains(listActiveJVMReachabilityFactsQuery, want) {
			t.Fatalf("listActiveJVMReachabilityFactsQuery missing %q:\n%s", want, listActiveJVMReachabilityFactsQuery)
		}
	}
}

func TestListActiveJVMReachabilityFactsPassesAPIPackageBoundsAndKeepsMatchingEvidence(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 1, 10, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{{
					"fact-jvm-match",
					"scope-jvm",
					"generation-jvm",
					"file",
					"file:repo:jvm:App.java",
					"1.0.0",
					"git",
					int64(0),
					"observed",
					"git",
					"file-key",
					"file:///repo/src/main/java/example/App.java",
					"record-jvm",
					observedAt,
					false,
					[]byte(`{"repo_id":"repo://example/jvm","relative_path":"src/main/java/example/App.java","parsed_file_data":{"lang":"java","imports":[{"source":"org.apache.logging.log4j.Logger"}]}}`),
				}},
			},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListActiveJVMReachabilityFacts(context.Background(), reducer.JVMReachabilityFactFilter{
		RepositoryIDs: []string{"repo://example/jvm", "repo://example/jvm"},
		APIPackages:   []string{" org.apache.logging.log4j ", "org.apache.logging.log4j"},
	})
	if err != nil {
		t.Fatalf("ListActiveJVMReachabilityFacts() error = %v, want nil", err)
	}
	if got, want := len(loaded), 1; got != want {
		t.Fatalf("ListActiveJVMReachabilityFacts() len = %d, want %d", got, want)
	}
	if got, want := loaded[0].FactID, "fact-jvm-match"; got != want {
		t.Fatalf("loaded fact ID = %q, want %q", got, want)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	repos, ok := db.queries[0].args[0].([]string)
	if !ok {
		t.Fatalf("repository arg type = %T, want []string", db.queries[0].args[0])
	}
	if got, want := strings.Join(repos, ","), "repo://example/jvm"; got != want {
		t.Fatalf("repository arg = %q, want %q", got, want)
	}
	apiPackages, ok := db.queries[0].args[1].([]string)
	if !ok {
		t.Fatalf("API package arg type = %T, want []string", db.queries[0].args[1])
	}
	if got, want := strings.Join(apiPackages, ","), "org.apache.logging.log4j"; got != want {
		t.Fatalf("API package arg = %q, want %q", got, want)
	}
	if got, want := db.queries[0].args[4], listFactsByKindPageSize; got != want {
		t.Fatalf("page size arg = %v, want %d", got, want)
	}
}
