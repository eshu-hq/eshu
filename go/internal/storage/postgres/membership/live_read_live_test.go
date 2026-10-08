// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membershipstore_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/membership"
	"github.com/eshu-hq/eshu/go/internal/scope/selection"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	membershipstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/membership"
)

// TestLiveFilterParityLive proves the SQL live filter in
// ReadLiveScopeObservations keeps exactly the rows selection.Live keeps, at
// the window boundary and around it, so the Go predicate and the SQL cannot
// drift. It shares TestObservationStoreLive's DSN gate.
func TestLiveFilterParityLive(t *testing.T) {
	ctx, sqlDB, store := openLiveStore(t)
	now := time.Date(2026, 10, 8, 6, 0, 0, 0, time.UTC)
	const scopeID = "scope:parity"

	cases := []struct {
		window time.Duration
		age    time.Duration
	}{
		{window: 48 * time.Hour, age: 20 * time.Hour},
		{window: 48 * time.Hour, age: 48 * time.Hour},
		{window: 48 * time.Hour, age: 48*time.Hour - time.Microsecond},
		{window: 48 * time.Hour, age: 48*time.Hour + time.Microsecond},
		{window: 48 * time.Hour, age: 50 * time.Hour},
		{window: time.Hour, age: time.Hour},
		{window: time.Hour, age: time.Hour + time.Second},
		{window: time.Second, age: 0},
		{window: time.Hour, age: -time.Minute},
	}
	var want []string
	for i, tc := range cases {
		selector := membership.NewGitHubOrgSelector("githubOrg", "acme", nil, false, membership.GitHubAppPrincipal("1", fmt.Sprint(i)))
		batch := membership.Batch{
			Selector: selector, EvaluatedAt: now.Add(-tc.age), LivenessWindow: tc.window,
			Rows: []membership.Row{{ScopeID: scopeID, State: membership.StateNotListed}},
		}
		if err := store.UpsertObservations(ctx, batch); err != nil {
			t.Fatalf("case %d UpsertObservations() error = %v", i, err)
		}
		if selection.Live(selection.Observation{EvaluatedAt: batch.EvaluatedAt, LivenessWindow: tc.window}, now) {
			want = append(want, selector.ID)
		}
	}
	slices.Sort(want)

	live, err := membershipstore.ReadLiveScopeObservations(ctx, postgres.SQLDB{DB: sqlDB}, scopeID, now)
	if err != nil {
		t.Fatalf("ReadLiveScopeObservations() error = %v", err)
	}
	got := make([]string, 0, len(live))
	for _, observation := range live {
		got = append(got, observation.SelectorID)
		if !selection.Live(observation, now) {
			t.Fatalf("SQL kept %+v, which selection.Live rejects", observation)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("SQL live rows = %v, Go live rows = %v", got, want)
	}
	if len(want) == 0 || len(want) == len(cases) {
		t.Fatalf("parity cases kept %d of %d rows; the boundary set must split", len(want), len(cases))
	}
}
