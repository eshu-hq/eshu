// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// grantScopeCatalog is the fixture catalog the grant-scope tests map grants
// through: r_example is collected by the repo target, r_other by the org
// target, and r_lonely by no configured target.
var grantScopeCatalog = []querycontract.RepositoryCatalogEntry{
	{ID: "repository:r_example", Name: "example-repo", RepoSlug: "example-org/example-repo", ScopeID: "scope:s_example"},
	{ID: "repository:r_other", Name: "other-repo", RepoSlug: "example-org/other-repo", ScopeID: "scope:s_other"},
	{ID: "repository:r_lonely", Name: "lonely-repo", RepoSlug: "example-org/lonely-repo", ScopeID: "scope:s_lonely"},
}

func grantScopeHandler(store SecurityAlertCollectionStateStore) *Handler {
	return &Handler{
		Content:                      selectorMatchContent{entries: grantScopeCatalog},
		SecurityAlerts:               coverageRowsStore{rows: 0},
		SecurityAlertAggregates:      coverageRowsStore{rows: 0},
		SecurityAlertCollectionState: store,
	}
}

func grantScopedContext(repoIDs []string, scopeIDs []string) context.Context {
	return auth.ContextWithAuthContext(context.Background(), auth.AuthContext{
		Mode:                 auth.AuthModeScoped,
		AllowedRepositoryIDs: repoIDs,
		AllowedScopeIDs:      scopeIDs,
	})
}

// grantScopeUnanchoredRoutes are the three security-alert read routes without
// a repository anchor. The CVE anchor keeps the list route past its scope
// gate so the empty answer reaches the collection-state classifier.
func grantScopeUnanchoredRoutes() map[string]string {
	return map[string]string{
		"list":      "/api/v0/supply-chain/security-alerts/reconciliations?limit=10&cve_id=CVE-2026-0001",
		"count":     "/api/v0/supply-chain/security-alerts/reconciliations/count?cve_id=CVE-2026-0001",
		"inventory": "/api/v0/supply-chain/security-alerts/reconciliations/inventory?cve_id=CVE-2026-0001",
	}
}

// TestSecurityAlertGrantScopeCoverage pins the #8837 contract: a scoped
// token's unanchored empty answer is classified over the caller's granted
// repositories only, not every configured target. A grant no target covers
// reports repository_outside_target_set, and the target-state read covers
// only the granting targets.
func TestSecurityAlertGrantScopeCoverage(t *testing.T) {
	t.Parallel()

	healthy := SecurityAlertTargetState{LatestWorkItemStatus: "completed", CommittedGeneration: true}
	pending := SecurityAlertTargetState{LatestWorkItemStatus: "pending"}
	configs := []string{coverageTestRepoTarget, coverageTestOrgTarget}
	const cause = "missing_collector_completion"
	// grantScopeOrgBoth is one org target covering both granted repositories
	// (two repositories stay under the shard cap, so the base scope is kept).
	// It lets the unread-isolation cases share a target the way a real org
	// allowlist does.
	const grantScopeOrgBoth = `{"targets":[{"provider":"github_dependabot","scope":"org","scope_id":"sa-target-org-both","organization":"example-org","allowed_repositories":["example-org/example-repo","example-org/other-repo"]}]}`
	cases := []struct {
		name       string
		configs    []string
		repoIDs    []string
		scopeIDs   []string
		states     map[string]SecurityAlertTargetState
		targetSets map[string][]string
		want       coverageWant
		wantScope  string
	}{
		{
			name:      "in-target grant reads complete over its covering target only",
			repoIDs:   []string{"repository:r_example"},
			states:    map[string]SecurityAlertTargetState{"sa-target-repo": healthy, "sa-target-org": healthy},
			want:      coverageWant{"complete", nil, "exact", "fresh", "", "ready_zero_results"},
			wantScope: "sa-target-repo",
		},
		{
			name:      "scope grant resolves through the catalog scope id",
			scopeIDs:  []string{"scope:s_example"},
			states:    map[string]SecurityAlertTargetState{"sa-target-repo": healthy, "sa-target-org": healthy},
			want:      coverageWant{"complete", nil, "exact", "fresh", "", "ready_zero_results"},
			wantScope: "sa-target-repo",
		},
		{
			name:      "out-of-target grant reports repository outside target set",
			repoIDs:   []string{"repository:r_lonely"},
			states:    map[string]SecurityAlertTargetState{"sa-target-repo": healthy, "sa-target-org": healthy},
			want:      coverageWant{"not_configured", []string{"repository_outside_target_set"}, "fallback", "unavailable", cause, ""},
			wantScope: "",
		},
		{
			name:      "mixed grants report repository outside target set",
			repoIDs:   []string{"repository:r_example", "repository:r_lonely"},
			states:    map[string]SecurityAlertTargetState{"sa-target-repo": healthy, "sa-target-org": healthy},
			want:      coverageWant{"not_configured", []string{"repository_outside_target_set"}, "fallback", "unavailable", cause, ""},
			wantScope: "sa-target-repo",
		},
		{
			// Worst wins: the outside grant outranks the collecting
			// target, and the reasons name both.
			name:      "collecting target with an outside grant unions both reasons",
			repoIDs:   []string{"repository:r_example", "repository:r_lonely"},
			states:    map[string]SecurityAlertTargetState{"sa-target-repo": pending, "sa-target-org": healthy},
			want:      coverageWant{"not_configured", []string{"no_committed_generation", "repository_outside_target_set"}, "fallback", "unavailable", cause, ""},
			wantScope: "sa-target-repo",
		},
		{
			name:      "grant unknown to the catalog reports repository outside target set",
			repoIDs:   []string{"repository:r_unknown"},
			states:    map[string]SecurityAlertTargetState{"sa-target-repo": healthy, "sa-target-org": healthy},
			want:      coverageWant{"not_configured", []string{"repository_outside_target_set"}, "fallback", "unavailable", cause, ""},
			wantScope: "",
		},
		{
			// An unmapped grant poisons even a healthy granted set: the
			// caller may read a repository the classifier never evaluated.
			name:      "mapped grant beside an unknown grant is not complete",
			repoIDs:   []string{"repository:r_example", "repository:r_unknown"},
			states:    map[string]SecurityAlertTargetState{"sa-target-repo": healthy, "sa-target-org": healthy},
			want:      coverageWant{"not_configured", []string{"repository_outside_target_set"}, "fallback", "unavailable", cause, ""},
			wantScope: "sa-target-repo",
		},
		{
			// #8838 through the grant path: the generation recorded an
			// empty target set after the grant's repository joined, so the
			// grant reads as still collecting, not complete.
			name:       "grant added after the latest generation reads collecting",
			configs:    []string{coverageTestRepoTarget},
			repoIDs:    []string{"repository:r_example"},
			states:     map[string]SecurityAlertTargetState{"sa-target-repo": healthy},
			targetSets: map[string][]string{"sa-target-repo": {}},
			want:       coverageWant{"target_incomplete", []string{"no_committed_generation"}, "derived", "building", cause, ""},
			wantScope:  "sa-target-repo",
		},
		{
			// #8943 isolation through the grant path: other-repo's unread
			// row on their shared target must not fail example-repo's
			// answer.
			name:    "another grant's unread row does not fail this grant",
			configs: []string{grantScopeOrgBoth},
			repoIDs: []string{"repository:r_example"},
			states: map[string]SecurityAlertTargetState{"sa-target-org-both": {
				LatestWorkItemStatus: "completed", CommittedGeneration: true,
				UnreadTotal: 1, UnreadReasons: []string{"org_repository_read_failed"},
				UnreadRequested: map[string]string{"example-org/other-repo": "org_repository_read_failed"},
			}},
			want:      coverageWant{"complete", nil, "exact", "fresh", "", "ready_zero_results"},
			wantScope: "sa-target-org-both",
		},
		{
			name:    "own unread row fails only its own grant",
			configs: []string{grantScopeOrgBoth},
			repoIDs: []string{"repository:r_other"},
			states: map[string]SecurityAlertTargetState{"sa-target-org-both": {
				LatestWorkItemStatus: "completed", CommittedGeneration: true,
				UnreadTotal: 1, UnreadReasons: []string{"org_repository_read_failed"},
				UnreadRequested: map[string]string{"example-org/other-repo": "org_repository_read_failed"},
			}},
			want:      coverageWant{"target_incomplete", []string{"org_repository_read_failed"}, "derived", "fresh", "", ""},
			wantScope: "sa-target-org-both",
		},
	}
	for _, tc := range cases {
		for route, target := range grantScopeUnanchoredRoutes() {
			t.Run(tc.name+"/"+route, func(t *testing.T) {
				t.Parallel()
				tcConfigs := tc.configs
				if tcConfigs == nil {
					tcConfigs = configs
				}
				store := &fakeSecurityAlertCollectionState{configs: tcConfigs, states: tc.states, targetSets: tc.targetSets}
				ctx := grantScopedContext(tc.repoIDs, tc.scopeIDs)
				code, body, resp := serveCoverage(ctx, t, grantScopeHandler(store), target)
				if code != http.StatusOK {
					t.Fatalf("status = %d, want 200; body = %s", code, body)
				}
				assertCoverage(t, resp, tc.want)
				assertCoverageInvariant(t, resp)
				assertNoTargetIdentity(t, body)
				if got := store.scopeIDs(); got != tc.wantScope {
					t.Fatalf("state read scope ids = %q, want %q", got, tc.wantScope)
				}
			})
		}
	}
}

// TestSecurityAlertGrantSlugs pins the grant-to-slug mapping (#8837): grants
// resolve through catalog ids and ingestion scope ids, a git repository scope
// resolves to the repository it owns, slugs come from repo_slug or remote_url
// evidence, grants the catalog does not know are reported unmapped, a grant
// set past the bound fails with the bound error before any catalog read, and
// a catalog failure propagates.
func TestSecurityAlertGrantSlugs(t *testing.T) {
	t.Parallel()

	catalogErr := errors.New("catalog read failed")
	cases := []struct {
		name       string
		entries    []querycontract.RepositoryCatalogEntry
		contentErr error
		repoIDs    []string
		scopeIDs   []string
		wantSlugs  []string
		wantMapped bool
		wantErr    bool
		wantBound  bool
	}{
		{
			name:       "repository grant maps through the catalog id",
			entries:    grantScopeCatalog,
			repoIDs:    []string{"repository:r_example"},
			wantSlugs:  []string{"example-org/example-repo"},
			wantMapped: true,
		},
		{
			name:       "scope grant maps through the catalog scope id",
			entries:    grantScopeCatalog,
			scopeIDs:   []string{"scope:s_other"},
			wantSlugs:  []string{"example-org/other-repo"},
			wantMapped: true,
		},
		{
			name: "git repository scope grant resolves to the owned repository",
			entries: []querycontract.RepositoryCatalogEntry{{
				ID: "repository:r_example", RepoSlug: "example-org/example-repo",
			}},
			scopeIDs:   []string{querycontract.GitRepositoryScopePrefix + "repository:r_example"},
			wantSlugs:  []string{"example-org/example-repo"},
			wantMapped: true,
		},
		{
			name: "remote url evidences the slug when repo slug is empty",
			entries: []querycontract.RepositoryCatalogEntry{{
				ID: "repository:r_example", RemoteURL: "https://github.com/Example-Org/Example-Repo.git",
			}},
			repoIDs:    []string{"repository:r_example"},
			wantSlugs:  []string{"example-org/example-repo"},
			wantMapped: true,
		},
		{
			name: "one slug shared by two grants is reported once",
			entries: []querycontract.RepositoryCatalogEntry{{
				ID: "repository:r_example", RepoSlug: "example-org/example-repo", ScopeID: "scope:s_example",
			}},
			repoIDs:    []string{"repository:r_example"},
			scopeIDs:   []string{"scope:s_example"},
			wantSlugs:  []string{"example-org/example-repo"},
			wantMapped: true,
		},
		{
			name:       "unknown grant is unmapped",
			entries:    grantScopeCatalog,
			repoIDs:    []string{"repository:r_unknown"},
			wantMapped: false,
		},
		{
			name: "entry without slug evidence leaves its grant unmapped",
			entries: []querycontract.RepositoryCatalogEntry{{
				ID: "repository:r_bare",
			}},
			repoIDs:    []string{"repository:r_bare"},
			wantMapped: false,
		},
		{
			name: "scope grant to a slug-less catalog row leaves its grant unmapped",
			entries: []querycontract.RepositoryCatalogEntry{{
				ID: "repository:r_bare", ScopeID: "scope:s_bare",
			}},
			scopeIDs:   []string{"scope:s_bare"},
			wantMapped: false,
		},
		{
			name:       "non-repository scope grant names no repository and is ignored",
			entries:    grantScopeCatalog,
			scopeIDs:   []string{"scope:s_unrelated"},
			wantSlugs:  nil,
			wantMapped: true,
		},
		{
			name:       "catalog failure propagates",
			entries:    grantScopeCatalog,
			contentErr: catalogErr,
			repoIDs:    []string{"repository:r_example"},
			wantErr:    true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := &Handler{Content: selectorMatchContent{entries: tc.entries, err: tc.contentErr}}
			access := querycontract.RepositoryAccessFilter{
				AllowedRepositoryIDs: tc.repoIDs,
				AllowedScopeIDs:      tc.scopeIDs,
			}
			slugs, unmapped, err := h.securityAlertGrantSlugs(context.Background(), access)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("err = nil, want catalog failure")
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if unmapped == tc.wantMapped {
				t.Fatalf("unmapped = %v, want mapped = %v", unmapped, tc.wantMapped)
			}
			if got, want := strings.Join(slugs, ","), strings.Join(tc.wantSlugs, ","); got != want {
				t.Fatalf("slugs = %q, want %q", got, want)
			}
			if !slices.IsSorted(slugs) {
				t.Fatalf("slugs %q are not sorted", slugs)
			}
		})
	}
}

// TestSecurityAlertGrantSlugsBound proves a grant set past the catalog bound
// fails with the bound error before any catalog read, so an over-granted
// token fails closed instead of silently dropping grants.
func TestSecurityAlertGrantSlugsBound(t *testing.T) {
	t.Parallel()

	ids := make([]string, securityAlertGrantCatalogMaxIDs+1)
	for i := range ids {
		ids[i] = fmt.Sprintf("repository:r_grant_%d", i)
	}
	h := &Handler{Content: selectorMatchContent{entries: grantScopeCatalog}}
	access := querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: ids}
	_, _, err := h.securityAlertGrantSlugs(context.Background(), access)
	if !errors.Is(err, ErrSecurityAlertCollectionStateBound) {
		t.Fatalf("err = %v, want the collection-state bound error", err)
	}
}
