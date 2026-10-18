// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membershipstore_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/membership"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
	membershipstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/membership"
)

func TestObservationStoreUpsertClassificationsBindsOneSortedStatement(t *testing.T) {
	t.Parallel()

	database := &fake.ExecQueryer{}
	err := membershipstore.NewObservationStore(database).UpsertClassifications(
		context.Background(),
		testSelector.ID,
		[]membership.ClassificationRow{
			{ScopeID: "git-repository-scope:c", Classification: membership.UpstreamTransferred, SuccessorSlug: "archive/c"},
			{ScopeID: "git-repository-scope:a", Classification: membership.UpstreamRenamed, SuccessorSlug: "acme/a-v2"},
			{ScopeID: "git-repository-scope:b", Classification: membership.UpstreamNotFoundOrInaccessible},
		},
		evaluatedAt,
	)
	if err != nil {
		t.Fatalf("UpsertClassifications() error = %v", err)
	}
	if got := len(database.Execs); got != 1 {
		t.Fatalf("exec count = %d, want one statement per batch", got)
	}
	want := []any{
		testSelector.ID, evaluatedAt.UTC(),
		[]string{"git-repository-scope:a", "git-repository-scope:b", "git-repository-scope:c"},
		[]string{"renamed", "not_found_or_inaccessible", "transferred"},
		[]string{"acme/a-v2", "", "archive/c"},
	}
	if got := database.Execs[0].Args; !reflect.DeepEqual(got, want) {
		t.Fatalf("bound args = %#v, want %#v", got, want)
	}
}

func TestObservationStoreUpsertClassificationsQueryGuards(t *testing.T) {
	t.Parallel()

	query := membershipstore.UpsertClassificationsQuery
	for _, want := range []string{
		"UPDATE repository_selection_observations AS o",
		"upstream_classification = s.classification",
		"successor_slug = NULLIF(s.successor_slug, '')",
		"classified_at = $2::timestamptz",
		"AND o.selector_id = $1",
		"AND o.state = 'not_listed'",
		"(o.classified_at IS NULL OR o.classified_at <= $2::timestamptz)",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("classifications query missing %q: %s", want, query)
		}
	}
}

func TestObservationStoreUpsertClassificationsRejectsBadRows(t *testing.T) {
	t.Parallel()

	store := membershipstore.NewObservationStore(&fake.ExecQueryer{})
	for _, tc := range []struct {
		name string
		id   string
		rows []membership.ClassificationRow
	}{
		{name: "blank selector", id: " ", rows: []membership.ClassificationRow{{ScopeID: "s", Classification: membership.UpstreamRenamed}}},
		{name: "blank scope", id: testSelector.ID, rows: []membership.ClassificationRow{{ScopeID: " ", Classification: membership.UpstreamRenamed}}},
		{name: "deleted is never stored", id: testSelector.ID, rows: []membership.ClassificationRow{{ScopeID: "s", Classification: "deleted"}}},
		{name: "empty is never stored", id: testSelector.ID, rows: []membership.ClassificationRow{{ScopeID: "s"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := store.UpsertClassifications(context.Background(), tc.id, tc.rows, evaluatedAt); err == nil {
				t.Fatalf("UpsertClassifications(%s) error = nil, want rejection", tc.name)
			}
		})
	}
	if err := store.UpsertClassifications(context.Background(), testSelector.ID,
		[]membership.ClassificationRow{{ScopeID: "s", Classification: membership.UpstreamRenamed}}, evaluatedAt); err != nil {
		t.Fatalf("UpsertClassifications() error = %v", err)
	}
}

func TestObservationStoreUpsertClassificationsEmptyWritesNothing(t *testing.T) {
	t.Parallel()

	database := &fake.ExecQueryer{}
	if err := membershipstore.NewObservationStore(database).UpsertClassifications(
		context.Background(), testSelector.ID, nil, evaluatedAt,
	); err != nil {
		t.Fatalf("UpsertClassifications(nil) error = %v", err)
	}
	if len(database.Execs) != 0 {
		t.Fatalf("exec count = %d, want no statement for an empty batch", len(database.Execs))
	}
	if err := membershipstore.NewObservationStore(nil).UpsertClassifications(
		context.Background(), testSelector.ID,
		[]membership.ClassificationRow{{ScopeID: "s", Classification: membership.UpstreamRenamed}}, evaluatedAt,
	); err == nil {
		t.Fatal("UpsertClassifications(nil store) error = nil, want an error")
	}
}

func TestObservationStoreUpsertClearsProbeVerdictOnListedState(t *testing.T) {
	t.Parallel()

	upsertSQL := membershipstore.UpsertObservationsQuery
	for _, want := range []string{
		"upstream_classification = CASE WHEN EXCLUDED.state = 'not_listed' THEN o.upstream_classification ELSE NULL END",
		"successor_slug = CASE WHEN EXCLUDED.state = 'not_listed' THEN o.successor_slug ELSE NULL END",
		"classified_at = CASE WHEN EXCLUDED.state = 'not_listed' THEN o.classified_at ELSE NULL END",
	} {
		if !strings.Contains(upsertSQL, want) {
			t.Fatalf("upsert query missing %q: %s", want, upsertSQL)
		}
	}
}
