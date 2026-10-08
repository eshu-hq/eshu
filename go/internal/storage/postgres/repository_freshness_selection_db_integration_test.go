// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// DSN-gated live-Postgres proof for the #7625 not_selected freshness verdict.
// It writes observations through the real phase-1 writer
// (membershipstore.ObservationStore.UpsertObservations) over two collector
// cycles and reads them back through the real ReadRepositoryFreshness path,
// so the writer's counter math, the reader's scope_id lookup, and
// selection.Summarize are proven together against PostgreSQL. It uses the
// same ESHU_POSTGRES_DSN gate and throwaway schema as
// TestReadRepositoryFreshnessLiveDB.

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/membership"
	"github.com/eshu-hq/eshu/go/internal/scope/selection"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	membershipstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/membership"
)

// TestReadRepositoryFreshnessSelectionLiveDB is the #7625 QA shape: the old
// scope of a renamed repository and a transferred repository drop out of the
// complete boatsgroup listing while a third repository stays listed. After
// one cycle nothing changes; after two cycles spanning the interval the two
// dropped scopes render not_selected and the listed scope stays current.
func TestReadRepositoryFreshnessSelectionLiveDB(t *testing.T) {
	dsn := repositoryFreshnessDBIntegrationDSN()
	if dsn == "" {
		t.Skipf("%s is not set; skipping repository freshness selection Postgres proof", repositoryFreshnessDBIntegrationDSNEnv)
	}

	ctx := context.Background()
	db := openRepositoryFreshnessDBIntegrationSchema(t, ctx, dsn)
	writer := membershipstore.NewObservationStore(SQLDB{DB: db})
	reader := NewRepositoryFreshnessStore(SQLDB{DB: db})

	interval := 5 * time.Minute
	cycle1 := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	cycle2 := cycle1.Add(interval)
	selector := membership.NewGitHubOrgSelector("githubOrg", "boatsgroup", nil, false)

	fixtures := map[string]freshnessScopeFixture{}
	for _, name := range []string{"renamed-old", "transferred", "listed", "archived", "unobserved"} {
		fx := freshnessScopeFixture{
			scopeID:        "scope-sel-" + name,
			generationID:   "gen-sel-" + name,
			repoID:         "repository:sel-" + name,
			repoSlug:       "boatsgroup/" + name,
			observedCommit: "c0ffee" + name,
			now:            cycle1.Add(-time.Hour),
		}
		seedRepositoryFreshnessScope(t, ctx, db, fx)
		fixtures[name] = fx
	}

	// The listing before the rename and transfer, so the dropped scopes carry
	// a last_listed_at like production rows do.
	upsert := func(at time.Time, rows ...membership.Row) {
		t.Helper()
		if err := writer.UpsertObservations(ctx, membership.Batch{Selector: selector, EvaluatedAt: at, Interval: interval, Rows: rows}); err != nil {
			t.Fatalf("UpsertObservations(%v) error = %v", at, err)
		}
	}
	upsert(cycle1.Add(-interval),
		membership.Row{ScopeID: fixtures["renamed-old"].scopeID, State: membership.StateSelected},
		membership.Row{ScopeID: fixtures["transferred"].scopeID, State: membership.StateSelected},
		membership.Row{ScopeID: fixtures["listed"].scopeID, State: membership.StateSelected},
	)
	dropped := func(at time.Time) {
		upsert(at,
			membership.Row{ScopeID: fixtures["renamed-old"].scopeID, State: membership.StateNotListed},
			membership.Row{ScopeID: fixtures["transferred"].scopeID, State: membership.StateNotListed},
			membership.Row{ScopeID: fixtures["listed"].scopeID, State: membership.StateSelected},
			membership.Row{ScopeID: fixtures["archived"].scopeID, State: membership.StateArchivedExcluded},
		)
	}

	read := func(name string, now time.Time) statuspkg.RepositoryFreshnessSnapshot {
		t.Helper()
		reader.now = func() time.Time { return now }
		snapshot, err := reader.ReadRepositoryFreshness(ctx, fixtures[name].repoID)
		if err != nil {
			t.Fatalf("ReadRepositoryFreshness(%s) error = %v", name, err)
		}
		return snapshot
	}
	verdict := func(name string, now time.Time) statuspkg.RepositoryFreshnessVerdict {
		t.Helper()
		return statuspkg.ComputeRepositoryFreshnessVerdict(read(name, now), "")
	}

	dropped(cycle1)
	for _, name := range []string{"renamed-old", "transferred"} {
		snapshot := read(name, cycle1)
		if snapshot.Selection == nil || snapshot.Selection.State != selection.AggregatePending {
			t.Fatalf("%s after one cycle: Selection = %+v, want pending", name, snapshot.Selection)
		}
		if got := statuspkg.ComputeRepositoryFreshnessVerdict(snapshot, ""); got != statuspkg.RepositoryFreshnessCurrent {
			t.Fatalf("%s after one cycle: verdict = %s, want current (one cycle is not confirmation)", name, got)
		}
	}
	if got := verdict("archived", cycle1); got != statuspkg.RepositoryFreshnessNotSelected {
		t.Fatalf("archived after one cycle: verdict = %s, want not_selected (positive listing evidence is immediate)", got)
	}

	dropped(cycle2)
	for _, name := range []string{"renamed-old", "transferred"} {
		snapshot := read(name, cycle2)
		sel := snapshot.Selection
		if sel == nil || sel.State != selection.AggregateNotSelected || sel.Reason != selection.StateNotListed {
			t.Fatalf("%s after two cycles: Selection = %+v, want not_selected/not_listed", name, sel)
		}
		if !sel.UnlistedSince.Equal(cycle1) || !sel.LastListedAt.Equal(cycle1.Add(-interval)) || !sel.EvaluatedAt.Equal(cycle2) {
			t.Fatalf("%s after two cycles: timestamps = %+v, want unlisted_since %v last_listed %v evaluated %v", name, sel, cycle1, cycle1.Add(-interval), cycle2)
		}
		if got := statuspkg.ComputeRepositoryFreshnessVerdict(snapshot, ""); got != statuspkg.RepositoryFreshnessNotSelected {
			t.Fatalf("%s after two cycles: verdict = %s, want not_selected", name, got)
		}
	}
	if got := verdict("listed", cycle2); got != statuspkg.RepositoryFreshnessCurrent {
		t.Fatalf("listed scope: verdict = %s, want current", got)
	}
	if snapshot := read("unobserved", cycle2); snapshot.Selection != nil {
		t.Fatalf("scope with no observation row: Selection = %+v, want nil", snapshot.Selection)
	}

	// The selector stops evaluating: three intervals after its last cycle its
	// rows still count, past that they are stale and the verdict reverts.
	if got := verdict("renamed-old", cycle2.Add(3*interval)); got != statuspkg.RepositoryFreshnessNotSelected {
		t.Fatalf("renamed-old at the liveness edge: verdict = %s, want not_selected", got)
	}
	if got := verdict("renamed-old", cycle2.Add(3*interval+time.Second)); got != statuspkg.RepositoryFreshnessCurrent {
		t.Fatalf("renamed-old with only stale rows: verdict = %s, want current", got)
	}
}
