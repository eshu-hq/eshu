// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membershipstore_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7765 disposable PostgreSQL 18 proof of the gitlab_group path-prefix partition on KnownScopes: only the nested group's git repository scopes are known (sibling group, lookalike prefix, underscore-prefix scope, another host's same-path scope, repository_ref, and remote-less scopes drop out), a gitlab_group upsert round-trips through the widened selector_kind CHECK, and the bootstrap applies twice. Enrolled in the live-postgres-readiness runner; 2.0-2.1s of test time on postgres:18-alpine on 2026-10-15; wall time is logged, not gated
//eshu:readiness:env ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE ESHU_GENERATION_RETENTION_PROOF_DSN

import (
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/membership"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestKnownScopesPathPrefixLive proves the gitlab_group known-scope read
// against real Postgres: only the nested group's git repository scopes are
// known. The sibling group, the lookalike prefix, the underscore-prefix
// scope, another host's same-path scopes, repository_ref scopes and
// remote-less scopes all drop out. A gitlab_group upsert round-trips
// through the widened selector_kind CHECK.
//
// Runs in the live-postgres-readiness runner; see TestObservationStoreLive.
func TestKnownScopesPathPrefixLive(t *testing.T) {
	ctx, sqlDB, store := openLiveStore(t)
	if err := postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: sqlDB}); err != nil {
		t.Fatalf("second bootstrap (migration idempotency) error = %v", err)
	}

	seedScopeRemote(ctx, t, sqlDB, "scope:gl-a", "repository", "git", "acme/platform/a", "https://gitlab.com/acme/platform/a")
	seedScopeRemote(ctx, t, sqlDB, "scope:gl-sub", "repository", "git", "acme/platform/sub/b", "https://gitlab.com/acme/platform/sub/b")
	seedScopeRemote(ctx, t, sqlDB, "scope:gl-deep", "repository", "git", "Acme/Platform/Sub/Deep/c", "https://gitlab.com/acme/platform/sub/deep/c")
	seedScopeRemote(ctx, t, sqlDB, "scope:gl-other", "repository", "git", "acme/other/x", "https://gitlab.com/acme/other/x")
	seedScopeRemote(ctx, t, sqlDB, "scope:gl-platformer", "repository", "git", "acme/platformer/y", "https://gitlab.com/acme/platformer/y")
	seedScopeRemote(ctx, t, sqlDB, "scope:gl-underscore", "repository", "git", "acme_platform/z", "https://gitlab.com/acme_platform/z")
	seedScopeRemote(ctx, t, sqlDB, "scope:gh-same-path", "repository", "git", "acme/platform/a", "https://github.com/acme/platform/a")
	seedScopeRemote(ctx, t, sqlDB, "scope:gl-ref", "repository_ref", "git", "acme/platform/a", "https://gitlab.com/acme/platform/a")
	seedScopeRemote(ctx, t, sqlDB, "scope:gl-no-remote", "repository", "git", "acme/platform/noremote", "")

	known, err := store.KnownScopes(ctx, membership.Partition{Owner: "Acme/Platform", Host: "gitlab.com", PathPrefix: true})
	if err != nil {
		t.Fatalf("KnownScopes() error = %v", err)
	}
	var got []string
	for _, scope := range known {
		got = append(got, scope.ScopeID)
	}
	slices.Sort(got)
	want := []string{"scope:gl-a", "scope:gl-deep", "scope:gl-sub"}
	if !slices.Equal(got, want) {
		t.Fatalf("KnownScopes() = %v, want %v", got, want)
	}

	selector := membership.NewGitLabGroupSelector("gitlabGroup", "gitlab.com", "acme/platform", nil, false, "")
	now := time.Date(2026, 10, 8, 6, 0, 0, 0, time.UTC)
	result := membership.Evaluate(membership.Input{
		Selector: selector, Now: now, LivenessWindow: testWindow,
		Listing: membership.Listing{Complete: true, Repositories: []membership.ListedRepository{
			{ScopeID: "scope:gl-a", Slug: "gitlab/acme/platform/a", GitHubID: 101, State: membership.StateSelected},
		}},
		Known: known,
	})
	if result.Outcome != membership.OutcomeEvaluated {
		t.Fatalf("outcome = %q, want %q", result.Outcome, membership.OutcomeEvaluated)
	}
	if err := store.UpsertObservations(ctx, result.Batch); err != nil {
		t.Fatalf("UpsertObservations() error = %v", err)
	}
	assertStoredEqualsProjection(ctx, t, store, selector.ID, result.Projected)
}
