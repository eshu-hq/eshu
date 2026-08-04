// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestListCICDRunFactsForRunKeysUsesBoundedTombstoneAwareHistory(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{
		cicdHistoryFactRow("run-fact", "gen-prior", "ci.run", "run-key", false),
	}}}}
	store := NewFactStore(db)
	loaded, err := store.ListCICDRunFactsForRunKeys(
		context.Background(),
		"scope-ci",
		"gen-current",
		[]string{" github_actions ", "github_actions"},
		[]string{"run-1", "run-1"},
		[]string{"", "1"},
	)
	if err != nil {
		t.Fatalf("ListCICDRunFactsForRunKeys() error = %v, want nil", err)
	}
	if got, want := len(loaded), 1; got != want {
		t.Fatalf("loaded facts = %d, want %d", got, want)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"FROM UNNEST($3::text[], $4::text[], $5::text[])",
		"ranked_run_facts AS MATERIALIZED",
		"PARTITION BY fact.fact_kind, fact.stable_fact_key",
		"fact.fact_kind = ANY($6::text[])",
		"generation.status IN ('active', 'completed', 'superseded')",
		"< (target_generation.ingested_at, $2)",
		"WHERE fact.fact_rank = 1",
		"AND fact.is_tombstone = FALSE",
		"ranked_deployment_facts AS MATERIALIZED",
		"fact.fact_kind = 'ci.deployment_event'",
		"LIMIT $7",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("historical query missing %q:\n%s", want, query)
		}
	}
	if got, want := db.queries[0].args[6], maxCICDRunHistoricalFacts+1; got != want {
		t.Fatalf("historical limit = %v, want %d", got, want)
	}
}

func TestListPreviousCICDRunCorrelationFactsUsesImmediatePredecessor(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{
		cicdHistoryFactRow(
			"prior-correlation",
			"gen-prior",
			"reducer_ci_cd_run_correlation",
			"correlation-key",
			false,
		),
	}}}}
	store := NewFactStore(db)
	loaded, err := store.ListPreviousCICDRunCorrelationFacts(
		context.Background(),
		"scope-ci",
		"gen-current",
	)
	if err != nil {
		t.Fatalf("ListPreviousCICDRunCorrelationFacts() error = %v, want nil", err)
	}
	if got, want := len(loaded), 1; got != want {
		t.Fatalf("loaded facts = %d, want %d", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"previous_generation AS MATERIALIZED",
		"ORDER BY generation.ingested_at DESC, generation.generation_id DESC",
		"LIMIT 1",
		"previous_generation.generation_id = fact.generation_id",
		"fact.fact_kind = 'reducer_ci_cd_run_correlation'",
		"LIMIT $3",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("previous-snapshot query missing %q:\n%s", want, query)
		}
	}
	if strings.Contains(query, "fact_kind = 'reducer_ci_cd_run_correlation'\n    ORDER BY") {
		t.Fatalf("previous generation selection must not skip an empty immediate predecessor:\n%s", query)
	}
}

func TestCleanCICDRunHistoryKeysRejectsMisalignedColumns(t *testing.T) {
	t.Parallel()

	_, err := cleanCICDRunHistoryKeys(
		[]string{"github_actions"},
		[]string{"run-1", "run-2"},
		[]string{"1"},
	)
	if err == nil {
		t.Fatal("cleanCICDRunHistoryKeys() error = nil, want misaligned-column rejection")
	}
}

func TestCICDRunHistoryReadsRejectResultsOverSafetyCaps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		limit int
		load  func(*FactStore) error
		kind  string
	}{
		{
			name:  "historical run facts",
			limit: maxCICDRunHistoricalFacts,
			kind:  "ci.run",
			load: func(store *FactStore) error {
				_, err := store.ListCICDRunFactsForRunKeys(
					context.Background(),
					"scope-ci",
					"gen-current",
					[]string{"github_actions"},
					[]string{"run-1"},
					[]string{"1"},
				)
				return err
			},
		},
		{
			name:  "previous correlation snapshot",
			limit: maxPreviousCICDRunCorrelationFacts,
			kind:  "reducer_ci_cd_run_correlation",
			load: func(store *FactStore) error {
				_, err := store.ListPreviousCICDRunCorrelationFacts(
					context.Background(),
					"scope-ci",
					"gen-current",
				)
				return err
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			rows := make([][]any, 0, test.limit+1)
			for index := 0; index <= test.limit; index++ {
				rows = append(rows, cicdHistoryFactRow(
					fmt.Sprintf("fact-%d", index),
					"gen-prior",
					test.kind,
					fmt.Sprintf("key-%d", index),
					false,
				))
			}
			db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: rows}}}

			err := test.load(NewFactStore(db))
			if err == nil || !strings.Contains(err.Error(), "exceeds safety cap") {
				t.Fatalf("load error = %v, want safety-cap rejection", err)
			}
		})
	}
}

func cicdHistoryFactRow(
	factID string,
	generationID string,
	factKind string,
	stableFactKey string,
	isTombstone bool,
) []any {
	return []any{
		factID,
		"scope-ci",
		generationID,
		factKind,
		stableFactKey,
		"1.0.0",
		"ci_cd_run",
		int64(0),
		"reported",
		"ci_cd_run",
		stableFactKey,
		"",
		"record-1",
		time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC),
		isTombstone,
		[]byte(`{"provider":"github_actions","run_id":"run-1","run_attempt":"1"}`),
	}
}
