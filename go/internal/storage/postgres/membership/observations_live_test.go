// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membershipstore_test

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/membership"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	membershipstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/membership"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestObservationStoreLive proves the repository selection observation
// contract (#7625) against real Postgres:
//
//  1. KnownScopes reads only the org's git repository scopes, matching the
//     org case-insensitively and excluding other orgs, repository_ref scopes,
//     and other collectors.
//  2. Over three cycles (first miss, confirming miss, relist) the stored rows
//     equal the projection membership.Evaluate computed, so the gauge needs
//     no read back.
//  3. Replaying a batch, or writing an older one, changes nothing.
//  4. Two selectors on one org keep separate rows, and concurrent replicas of
//     one selector writing the same evaluation in opposite row orders neither
//     deadlock nor double-count.
//
// It runs in the live-postgres-readiness runner. Run locally with a disposable
// PostgreSQL 18 administrative database:
//
//	ESHU_GENERATION_RETENTION_PROOF_DSN=postgresql://postgres:postgres@localhost:<port>/postgres?sslmode=disable \
//	ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE=1 \
//	  go test ./internal/storage/postgres/membership -run ObservationStoreLive -count=1
func TestObservationStoreLive(t *testing.T) {
	dsn := os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DSN")
	optIn := os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE")
	ctx, sqlDB := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 2*time.Minute)
	if err := postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: sqlDB}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}
	store := membershipstore.NewObservationStore(postgres.SQLDB{DB: sqlDB})

	for i := range 20 {
		org := "acme"
		if i%2 == 1 {
			org = "Acme"
		}
		seedScope(ctx, t, sqlDB, fmt.Sprintf("scope:acme-%02d", i), "repository", "git", fmt.Sprintf("%s/repo-%02d", org, i))
	}
	seedScope(ctx, t, sqlDB, "scope:other", "repository", "git", "other/repo")
	seedScope(ctx, t, sqlDB, "scope:acme-ref", "repository_ref", "git", "acme/repo-00")
	seedScope(ctx, t, sqlDB, "scope:acme-webhook", "repository", "github_webhook", "acme/repo-00")

	known, err := store.KnownScopes(ctx, "ACME")
	if err != nil {
		t.Fatalf("KnownScopes() error = %v", err)
	}
	if len(known) != 20 {
		t.Fatalf("KnownScopes() = %d scopes, want the 20 acme git repository scopes: %+v", len(known), known)
	}

	selectorA := membership.NewGitHubOrgSelector("githubOrg", "acme", nil, false)
	start := time.Date(2026, 10, 8, 6, 0, 0, 0, time.UTC)
	listing := func(missing ...int) membership.Listing {
		out := membership.Listing{Complete: true}
		for i := range 20 {
			if slices.Contains(missing, i) {
				continue
			}
			out.Repositories = append(out.Repositories, membership.ListedRepository{
				ScopeID: fmt.Sprintf("scope:acme-%02d", i), Slug: fmt.Sprintf("acme/repo-%02d", i),
				GitHubID: int64(100 + i), State: membership.StateSelected,
			})
		}
		return out
	}
	var last membership.Result
	for cycle, missing := range [][]int{{3, 7}, {3, 7}, {7}} {
		prior, err := store.Observations(ctx, selectorA.ID)
		if err != nil {
			t.Fatalf("cycle %d Observations() error = %v", cycle, err)
		}
		last = membership.Evaluate(membership.Input{
			Selector: selectorA, Now: start.Add(time.Duration(cycle) * 10 * time.Minute),
			Listing: listing(missing...), Known: known, Prior: prior,
		})
		if last.Outcome != membership.OutcomeEvaluated {
			t.Fatalf("cycle %d outcome = %q", cycle, last.Outcome)
		}
		if err := store.UpsertObservations(ctx, last.Batch); err != nil {
			t.Fatalf("cycle %d UpsertObservations() error = %v", cycle, err)
		}
		assertStoredEqualsProjection(ctx, t, store, selectorA.ID, last.Projected)
	}
	stored := readByScope(ctx, t, store, selectorA.ID)
	if got := stored["scope:acme-07"]; !membership.Confirmed(got) || !got.LastListedAt.IsZero() {
		t.Fatalf("scope:acme-07 after three misses = %+v, want confirmed not_listed with no listing ever", got)
	}
	if got := stored["scope:acme-03"]; got.State != membership.StateSelected || got.UnlistedCycleCount != 0 || !got.FirstUnlistedAt.IsZero() || got.GitHubRepoID != 103 {
		t.Fatalf("relisted scope:acme-03 = %+v, want selected with reset counters and id 103", got)
	}

	if err := store.UpsertObservations(ctx, last.Batch); err != nil {
		t.Fatalf("replay UpsertObservations() error = %v", err)
	}
	stale := last.Batch
	stale.EvaluatedAt = start
	if err := store.UpsertObservations(ctx, stale); err != nil {
		t.Fatalf("stale UpsertObservations() error = %v", err)
	}
	assertStoredEqualsProjection(ctx, t, store, selectorA.ID, last.Projected)

	selectorB := membership.NewGitHubOrgSelector("githubOrg", "acme", []membership.Rule{{Kind: "regex", Value: "^acme/repo-0"}}, false)
	upsertReplicasConcurrently(ctx, t, store, selectorA, selectorB, known, start.Add(time.Hour))
	if got := len(readByScope(ctx, t, store, selectorB.ID)); got != 20 {
		t.Fatalf("selector B rows = %d, want 20", got)
	}
	for scopeID, observation := range readByScope(ctx, t, store, selectorA.ID) {
		if observation.State == membership.StateNotListed && observation.UnlistedCycleCount != 4 {
			t.Fatalf("selector A %s = %+v, want exactly one more miss from eight concurrent replicas", scopeID, observation)
		}
	}
}

// upsertReplicasConcurrently has eight goroutines write one evaluation each
// for selectorA (identical evaluated_at, rows in opposite orders) and
// selectorB (distinct evaluated_at), all at once.
func upsertReplicasConcurrently(
	ctx context.Context,
	t *testing.T,
	store membershipstore.ObservationStore,
	selectorA, selectorB membership.Selector,
	known []membership.KnownScope,
	at time.Time,
) {
	t.Helper()
	rows := make([]membership.Row, 0, len(known))
	for _, scope := range known {
		state := membership.StateSelected
		if scope.ScopeID == "scope:acme-07" {
			state = membership.StateNotListed
		}
		rows = append(rows, membership.Row{ScopeID: scope.ScopeID, State: state})
	}
	reversed := slices.Clone(rows)
	slices.Reverse(reversed)

	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range 8 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			batch := membership.Batch{Selector: selectorA, EvaluatedAt: at, Interval: 10 * time.Minute, Rows: rows}
			if i%2 == 1 {
				batch.Rows = reversed
			}
			if i >= 4 {
				batch = membership.Batch{Selector: selectorB, EvaluatedAt: at.Add(time.Duration(i) * time.Second), Interval: 10 * time.Minute, Rows: rows}
			}
			errs[i] = store.UpsertObservations(ctx, batch)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent UpsertObservations()[%d] error = %v", i, err)
		}
	}
}

func seedScope(ctx context.Context, t *testing.T, sqlDB *sql.DB, scopeID, kind, collectorKind, slug string) {
	t.Helper()
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, $2, 'git', $1, $3, $1, now(), now(), 'active', jsonb_build_object('repo_slug', $4::text))`,
		scopeID, kind, collectorKind, slug); err != nil {
		t.Fatalf("seed scope %s: %v", scopeID, err)
	}
}

func readByScope(ctx context.Context, t *testing.T, store membershipstore.ObservationStore, selectorID string) map[string]membership.Observation {
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

// assertStoredEqualsProjection fails unless the stored rows equal want field
// by field, comparing times with Equal.
func assertStoredEqualsProjection(ctx context.Context, t *testing.T, store membershipstore.ObservationStore, selectorID string, want []membership.Observation) {
	t.Helper()
	stored, err := store.Observations(ctx, selectorID)
	if err != nil {
		t.Fatalf("Observations() error = %v", err)
	}
	byScopeID := func(a, b membership.Observation) int { return cmp.Compare(a.ScopeID, b.ScopeID) }
	slices.SortFunc(stored, byScopeID)
	want = slices.Clone(want)
	slices.SortFunc(want, byScopeID)
	if len(stored) != len(want) {
		t.Fatalf("stored %d rows, projected %d", len(stored), len(want))
	}
	for i := range want {
		got, exp := stored[i], want[i]
		if got.ScopeID != exp.ScopeID || got.State != exp.State || got.GitHubRepoID != exp.GitHubRepoID ||
			got.UnlistedCycleCount != exp.UnlistedCycleCount || got.EvaluationInterval != exp.EvaluationInterval ||
			!got.LastListedAt.Equal(exp.LastListedAt) || !got.FirstUnlistedAt.Equal(exp.FirstUnlistedAt) ||
			!got.EvaluatedAt.Equal(exp.EvaluatedAt) {
			t.Fatalf("stored %+v, projected %+v", got, exp)
		}
	}
}
