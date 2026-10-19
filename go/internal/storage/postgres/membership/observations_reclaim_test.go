// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membershipstore_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/membership"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
	membershipstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/membership"
)

// statesRows stages one RETURNING batch of deleted-row states.
func statesRows(states ...string) fake.Rows {
	data := make([][]any, len(states))
	for i, state := range states {
		data[i] = []any{state}
	}
	return fake.Rows{Data: data}
}

func TestDeleteExpiredObservationsCountsReclaimedNotListedSeparately(t *testing.T) {
	t.Parallel()

	database := &fake.ExecQueryer{QueryResponses: []fake.Rows{
		statesRows("selected", "not_listed", "not_listed", "rule_excluded"),
	}}
	counts, err := membershipstore.NewObservationStore(database).DeleteExpiredObservations(context.Background(), evaluatedAt, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("DeleteExpiredObservations() error = %v", err)
	}
	if want := (membership.SweepCounts{Deleted: 4, NotListedReclaimed: 2}); counts != want {
		t.Fatalf("counts = %+v, want %+v", counts, want)
	}
	if len(database.Queries) != 1 {
		t.Fatalf("statements = %d, want 1 short batch", len(database.Queries))
	}
	want := []any{evaluatedAt.UTC(), int64(7 * 24 * 3600), membershipstore.ExpiredSweepBatchSize, "not_listed"}
	if got := database.Queries[0].Args; !reflect.DeepEqual(got, want) {
		t.Fatalf("bound args = %#v, want %#v", got, want)
	}
}

func TestUpsertSelectorCycleWritesAdvanceOnlyMarker(t *testing.T) {
	t.Parallel()

	database := &fake.ExecQueryer{ExecResults: sweepResults(1)}
	selector := membership.NewGitHubOrgSelector("githubOrg", "acme", nil, false, membership.GitHubAppPrincipal("9", "10"))
	err := membershipstore.NewObservationStore(database).UpsertSelectorCycle(context.Background(), selector, evaluatedAt, 48*time.Hour)
	if err != nil {
		t.Fatalf("UpsertSelectorCycle() error = %v", err)
	}
	if len(database.Execs) != 1 {
		t.Fatalf("statements = %d, want 1", len(database.Execs))
	}
	want := []any{selector.ID, "github_org", "acme", evaluatedAt.UTC(), int64(48 * 3600)}
	if got := database.Execs[0].Args; !reflect.DeepEqual(got, want) {
		t.Fatalf("bound args = %#v, want %#v", got, want)
	}
	query := membershipstore.UpsertSelectorCycleQuery
	for _, fragment := range []string{
		"INSERT INTO repository_selectors",
		"ON CONFLICT (selector_id) DO UPDATE",
		"WHERE repository_selectors.last_cycle_at < EXCLUDED.last_cycle_at",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("selector cycle query missing %q: %s", fragment, query)
		}
	}
}

func TestUpsertSelectorCycleRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	selector := membership.NewGitHubOrgSelector("githubOrg", "acme", nil, false, "app:9:10")
	cases := []struct {
		name     string
		store    membershipstore.ObservationStore
		selector membership.Selector
		at       time.Time
		window   time.Duration
		want     string
	}{
		{name: "no database", store: membershipstore.NewObservationStore(nil), selector: selector, at: evaluatedAt, window: time.Hour, want: "database is required"},
		{name: "blank selector id", store: membershipstore.NewObservationStore(&fake.ExecQueryer{}), selector: membership.Selector{Kind: "github_org", Owner: "acme"}, at: evaluatedAt, window: time.Hour, want: "selector id must not be blank"},
		{name: "unknown kind", store: membershipstore.NewObservationStore(&fake.ExecQueryer{}), selector: membership.Selector{ID: "x", Kind: "nope", Owner: "acme"}, at: evaluatedAt, window: time.Hour, want: "unsupported selector kind"},
		{name: "blank owner", store: membershipstore.NewObservationStore(&fake.ExecQueryer{}), selector: membership.Selector{ID: "x", Kind: "github_org"}, at: evaluatedAt, window: time.Hour, want: "selector owner must not be blank"},
		{name: "zero cycle time", store: membershipstore.NewObservationStore(&fake.ExecQueryer{}), selector: selector, window: time.Hour, want: "cycle time is required"},
		{name: "window too small", store: membershipstore.NewObservationStore(&fake.ExecQueryer{}), selector: selector, at: evaluatedAt, window: time.Millisecond, want: "liveness window"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.store.UpsertSelectorCycle(context.Background(), tc.selector, tc.at, tc.window)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("UpsertSelectorCycle() error = %v, want containing %q", err, tc.want)
			}
		})
	}
}
