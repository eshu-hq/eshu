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
		nil,
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
		"fact.fact_kind = ANY($7::text[])",
		"generation.status IN ('active', 'completed', 'superseded')",
		"< (target_generation.ingested_at, $2)",
		"WHERE fact.fact_rank = 1",
		"AND fact.is_tombstone = FALSE",
		"ranked_deployment_facts AS MATERIALIZED",
		"fact.fact_kind = 'ci.deployment_event'",
		"latest_workflow_image_facts AS MATERIALIZED",
		"fact.fact_kind = 'ci.workflow_image_evidence'",
		"SELECT * FROM latest_workflow_image_facts",
		"LIMIT $8",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("historical query missing %q:\n%s", want, query)
		}
	}
	if got, want := db.queries[0].args[7], maxCICDRunHistoricalFacts+1; got != want {
		t.Fatalf("historical limit = %v, want %d", got, want)
	}
	if got, want := db.queries[0].args[8], false; got != want {
		t.Fatalf("include scope snapshot = %v, want %v", got, want)
	}
}

func TestListCICDRunFactsForScopePatchIncludesEveryLatestLiveRun(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{}}}
	store := NewFactStore(db)
	_, err := store.ListCICDRunFactsForScopePatch(
		context.Background(),
		"scope-ci",
		"gen-current",
		nil,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("ListCICDRunFactsForScopePatch() error = %v, want nil", err)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"WHERE $9::boolean",
		"fact.fact_kind = 'ci.run'",
		"fact.fact_rank = 1",
		"fact.is_tombstone = FALSE",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("scope-patch query missing %q:\n%s", want, query)
		}
	}
	if got, want := db.queries[0].args[8], true; got != want {
		t.Fatalf("include scope snapshot = %v, want %v", got, want)
	}
}

func TestListCICDRunFactsForRunKeysRoutesPayloadEmptyArtifactTombstones(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{
		cicdHistoryFactRow("artifact-prior", "gen-prior", "ci.artifact", "artifact-key", false),
	}}}}
	store := NewFactStore(db)
	loaded, err := store.ListCICDRunFactsForRunKeys(
		context.Background(),
		"scope-ci",
		"gen-current",
		nil,
		nil,
		nil,
		[]string{" artifact-key ", "artifact-key"},
	)
	if err != nil {
		t.Fatalf("ListCICDRunFactsForRunKeys() error = %v, want nil", err)
	}
	if got, want := len(loaded), 1; got != want {
		t.Fatalf("loaded facts = %d, want %d", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"FROM UNNEST($6::text[]) AS tombstone(stable_fact_key)",
		"retained_run_facts AS MATERIALIZED",
		"ranked_tombstone_artifact_identities AS MATERIALIZED",
		"fact.fact_kind = 'ci.artifact'",
		"fact.is_tombstone = FALSE",
		"BTRIM(fact.payload->>'provider') <> ''",
		"BTRIM(fact.payload->>'run_id') <> ''",
		"latest_tombstone_artifact_identities AS MATERIALIZED",
		"effective_run_keys AS MATERIALIZED",
		"SELECT * FROM latest_tombstone_artifact_identities",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("artifact-tombstone history query missing %q:\n%s", want, query)
		}
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
		want  string
	}{
		{
			name:  "historical run facts",
			limit: maxCICDRunHistoricalFacts,
			kind:  "ci.run",
			want:  "for 1 run keys and 0 artifact tombstone keys",
			load: func(store *FactStore) error {
				_, err := store.ListCICDRunFactsForRunKeys(
					context.Background(),
					"scope-ci",
					"gen-current",
					[]string{"github_actions"},
					[]string{"run-1"},
					[]string{"1"},
					nil,
				)
				return err
			},
		},
		{
			name:  "historical artifact tombstone routing",
			limit: maxCICDRunHistoricalFacts,
			kind:  "ci.artifact",
			want:  "for 0 run keys and 1 artifact tombstone keys",
			load: func(store *FactStore) error {
				_, err := store.ListCICDRunFactsForRunKeys(
					context.Background(),
					"scope-ci",
					"gen-current",
					nil,
					nil,
					nil,
					[]string{"artifact-key"},
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
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("load error = %v, want substring %q", err, test.want)
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
