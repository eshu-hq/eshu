// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// DSN-gated live-Postgres proof for the #7625 not_selected freshness verdict.
// It writes observations through the real writer
// (membershipstore.ObservationStore.UpsertObservations) over collector cycles
// and reads them back through the real ReadRepositoryFreshness path, so the
// writer's state_since/state_cycle_count math, the reader's SQL live filter,
// the rule (d) latest-generation read, and selection.Summarize are proven
// together against PostgreSQL. It uses the same ESHU_POSTGRES_DSN gate and
// throwaway schema as TestReadRepositoryFreshnessLiveDB.

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
// complete boatsgroup listing, an archived repository is excluded, and a
// fourth repository stays listed. After one cycle nothing changes; after a
// second cycle at least selection.ConfirmationMinSpan later the dropped and
// archived scopes render not_selected unless a generation was observed after
// the exclusion started. Once the selector stops evaluating, its rows expire
// at the liveness window and the verdict reverts.
func TestReadRepositoryFreshnessSelectionLiveDB(t *testing.T) {
	dsn := repositoryFreshnessDBIntegrationDSN()
	if dsn == "" {
		t.Skipf("%s is not set; skipping repository freshness selection Postgres proof", repositoryFreshnessDBIntegrationDSNEnv)
	}

	ctx := context.Background()
	db := openRepositoryFreshnessDBIntegrationSchema(t, ctx, dsn)
	writer := membershipstore.NewObservationStore(SQLDB{DB: db})
	reader := NewRepositoryFreshnessStore(SQLDB{DB: db})

	window := 48 * time.Hour
	cycle1 := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	cycle2 := cycle1.Add(selection.ConfirmationMinSpan)
	selector := membership.NewGitHubOrgSelector("githubOrg", "boatsgroup", nil, false, membership.GitHubAppPrincipal("1", "2"))

	fixtures := map[string]freshnessScopeFixture{}
	for _, name := range []string{"renamed-old", "transferred", "listed", "archived", "reingested", "unobserved"} {
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
	// Something else ingested "reingested" after the exclusion started: a
	// later, non-active generation is enough to fail rule (d).
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, is_delta, observed_at, ingested_at, status, payload)
VALUES ('gen-sel-reingested-2', $1, 'push', false, $2, $2, 'failed', '{}'::jsonb)`,
		fixtures["reingested"].scopeID, cycle1.Add(time.Minute)); err != nil {
		t.Fatalf("insert later generation: %v", err)
	}

	upsert := func(at time.Time, rows ...membership.Row) {
		t.Helper()
		if err := writer.UpsertObservations(ctx, membership.Batch{Selector: selector, EvaluatedAt: at, LivenessWindow: window, Rows: rows}); err != nil {
			t.Fatalf("UpsertObservations(%v) error = %v", at, err)
		}
	}
	// The listing before the rename and transfer, so the dropped scopes carry
	// a last_listed_at like production rows do.
	beforeDrop := cycle1.Add(-selection.ConfirmationMinSpan)
	upsert(beforeDrop,
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
			membership.Row{ScopeID: fixtures["reingested"].scopeID, State: membership.StateRuleExcluded},
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
	for _, name := range []string{"renamed-old", "transferred", "archived", "reingested"} {
		snapshot := read(name, cycle1)
		if snapshot.Selection.State != selection.AggregatePendingConfirmation {
			t.Fatalf("%s after one cycle: Selection = %+v, want pending_confirmation", name, snapshot.Selection)
		}
		if got := statuspkg.ComputeRepositoryFreshnessVerdict(snapshot, ""); got != statuspkg.RepositoryFreshnessCurrent {
			t.Fatalf("%s after one cycle: verdict = %s, want current (one cycle is not confirmation)", name, got)
		}
	}

	dropped(cycle2)
	for _, name := range []string{"renamed-old", "transferred"} {
		sel := read(name, cycle2).Selection
		if sel.State != selection.AggregateNotSelected || sel.Reason != selection.StateNotListed || sel.LiveSelectorCount != 1 {
			t.Fatalf("%s after two cycles: Selection = %+v, want not_selected/not_listed over 1 selector", name, sel)
		}
		if !sel.StateSince.Equal(cycle1) || !sel.LastListedAt.Equal(beforeDrop) || !sel.EvaluatedAt.Equal(cycle2) {
			t.Fatalf("%s after two cycles: timestamps = %+v, want state_since %v last_listed %v evaluated %v", name, sel, cycle1, beforeDrop, cycle2)
		}
		if got := verdict(name, cycle2); got != statuspkg.RepositoryFreshnessNotSelected {
			t.Fatalf("%s after two cycles: verdict = %s, want not_selected", name, got)
		}
	}
	if got := verdict("archived", cycle2); got != statuspkg.RepositoryFreshnessNotSelected {
		t.Fatalf("archived after two cycles: verdict = %s, want not_selected", got)
	}
	if sel := read("reingested", cycle2).Selection; sel.State != selection.AggregateExcludedStillIngested {
		t.Fatalf("reingested: Selection = %+v, want excluded_still_ingested", sel)
	}
	if got := verdict("reingested", cycle2); got != statuspkg.RepositoryFreshnessCurrent {
		t.Fatalf("reingested: verdict = %s, want current (a later generation fails rule (d))", got)
	}
	if got := verdict("listed", cycle2); got != statuspkg.RepositoryFreshnessCurrent {
		t.Fatalf("listed scope: verdict = %s, want current", got)
	}
	if sel := read("unobserved", cycle2).Selection; sel != (selection.Summary{State: selection.AggregateUnknown}) {
		t.Fatalf("scope with no observation row: Selection = %+v, want unknown", sel)
	}

	// The selector stops evaluating: up to the window its rows still count,
	// past it they expire, the scope reads unknown, and the verdict reverts.
	if got := verdict("renamed-old", cycle2.Add(window)); got != statuspkg.RepositoryFreshnessNotSelected {
		t.Fatalf("renamed-old at the liveness edge: verdict = %s, want not_selected", got)
	}
	expiredAt := cycle2.Add(window + time.Second)
	if sel := read("renamed-old", expiredAt).Selection; sel.State != selection.AggregateUnknown {
		t.Fatalf("renamed-old past the window: Selection = %+v, want unknown", sel)
	}
	if got := verdict("renamed-old", expiredAt); got != statuspkg.RepositoryFreshnessCurrent {
		t.Fatalf("renamed-old with only expired rows: verdict = %s, want current", got)
	}
}
