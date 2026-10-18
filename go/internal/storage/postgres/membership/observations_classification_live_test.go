// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membershipstore_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8454 disposable PostgreSQL 18 proof of the upstream classification probe storage: classifications round-trip through the store read, a stale probe never overwrites a newer verdict, relisting a scope clears its verdict, and the CHECK rejects a verdict outside the closed set. Enrolled in the live-postgres-readiness runner; wall time is logged, not gated
//eshu:readiness:env ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE ESHU_GENERATION_RETENTION_PROOF_DSN

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/membership"
	membershipstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/membership"
)

// TestObservationStoreClassificationLive proves the #8454 probe storage
// contract against real Postgres:
//
//  1. UpsertClassifications verdicts round-trip through Observations with
//     the successor slug and probe time.
//  2. A probe older than the stored classified_at changes nothing.
//  3. Re-upserting the scope as selected clears the verdict: a verdict
//     belongs to a not_listed row.
//  4. Seeded violation: the CHECK rejects 'deleted' at the database, so a
//     404 never reads deleted even below the Go validation.
func TestObservationStoreClassificationLive(t *testing.T) {
	ctx, sqlDB, store := openLiveStore(t)

	selector := membership.NewGitHubOrgSelector("githubOrg", "acme", nil, false, testPrincipal)
	cycle := evaluatedAt
	if err := store.UpsertObservations(ctx, membership.Batch{
		Selector: selector, EvaluatedAt: cycle, LivenessWindow: testWindow,
		Rows: []membership.Row{
			{ScopeID: "scope:class-a", State: membership.StateSelected, GitHubRepoID: 11},
			{ScopeID: "scope:class-b", State: membership.StateNotListed, GitHubRepoID: 12},
			{ScopeID: "scope:class-c", State: membership.StateNotListed, GitHubRepoID: 13},
		},
	}); err != nil {
		t.Fatalf("UpsertObservations() error = %v", err)
	}
	probedAt := cycle.Add(time.Hour)
	if err := store.UpsertClassifications(ctx, selector.ID, []membership.ClassificationRow{
		{ScopeID: "scope:class-b", Classification: membership.UpstreamTransferred, SuccessorSlug: "archive/b"},
		{ScopeID: "scope:class-c", Classification: membership.UpstreamRenamed, SuccessorSlug: "acme/c-v2"},
	}, probedAt); err != nil {
		t.Fatalf("UpsertClassifications() error = %v", err)
	}
	byScope := observationsByScope(t, ctx, store, selector.ID)
	if got := byScope["scope:class-b"]; got.Classification != membership.UpstreamTransferred ||
		got.SuccessorSlug != "archive/b" || !got.ClassifiedAt.Equal(probedAt.UTC()) {
		t.Fatalf("class-b = %+v, want transferred/archive/b at %v", got, probedAt.UTC())
	}
	if got := byScope["scope:class-c"]; got.Classification != membership.UpstreamRenamed ||
		got.SuccessorSlug != "acme/c-v2" || !got.ClassifiedAt.Equal(probedAt.UTC()) {
		t.Fatalf("class-c = %+v, want renamed/acme/c-v2 at %v", got, probedAt.UTC())
	}
	if got := byScope["scope:class-a"]; got.Classification != "" || got.SuccessorSlug != "" || !got.ClassifiedAt.IsZero() {
		t.Fatalf("class-a = %+v, want no verdict on a selected row", got)
	}

	if err := store.UpsertClassifications(ctx, selector.ID, []membership.ClassificationRow{
		{ScopeID: "scope:class-b", Classification: membership.UpstreamRenamed, SuccessorSlug: "acme/b-v2"},
	}, cycle); err != nil {
		t.Fatalf("UpsertClassifications(stale) error = %v", err)
	}
	if got := observationsByScope(t, ctx, store, selector.ID)["scope:class-b"]; got.Classification != membership.UpstreamTransferred {
		t.Fatalf("class-b after stale probe = %+v, want the newer transferred verdict kept", got)
	}

	relistAt := probedAt.Add(time.Hour)
	if err := store.UpsertObservations(ctx, membership.Batch{
		Selector: selector, EvaluatedAt: relistAt, LivenessWindow: testWindow,
		Rows: []membership.Row{
			{ScopeID: "scope:class-a", State: membership.StateSelected, GitHubRepoID: 11},
			{ScopeID: "scope:class-b", State: membership.StateSelected, GitHubRepoID: 12},
			{ScopeID: "scope:class-c", State: membership.StateNotListed, GitHubRepoID: 13},
		},
	}); err != nil {
		t.Fatalf("UpsertObservations(relist) error = %v", err)
	}
	byScope = observationsByScope(t, ctx, store, selector.ID)
	if got := byScope["scope:class-b"]; got.Classification != "" || got.SuccessorSlug != "" || !got.ClassifiedAt.IsZero() {
		t.Fatalf("class-b after relist = %+v, want the verdict cleared", got)
	}
	if got := byScope["scope:class-c"]; got.Classification != membership.UpstreamRenamed {
		t.Fatalf("class-c after relist cycle = %+v, want the verdict carried on the still-unlisted row", got)
	}

	_, err := sqlDB.ExecContext(ctx, `
INSERT INTO repository_selection_observations (
    scope_id, selector_id, selector_kind, selector_owner, state,
    state_since, state_cycle_count, evaluated_at, liveness_window_seconds,
    upstream_classification
) VALUES (
    'scope:class-seed', $1, 'github_org', 'acme', 'not_listed',
    now(), 1, now(), 172800,
    'deleted'
)`, selector.ID)
	if err == nil || !strings.Contains(err.Error(), "repository_selection_observations_upstream_classification_check") {
		t.Fatalf("seeded deleted verdict error = %v, want the classification CHECK to reject it", err)
	}
}

func observationsByScope(t *testing.T, ctx context.Context, store membershipstore.ObservationStore, selectorID string) map[string]membership.Observation {
	t.Helper()
	observations, err := store.Observations(ctx, selectorID)
	if err != nil {
		t.Fatalf("Observations() error = %v", err)
	}
	byScope := make(map[string]membership.Observation, len(observations))
	for _, observation := range observations {
		byScope[observation.ScopeID] = observation
	}
	return byScope
}
