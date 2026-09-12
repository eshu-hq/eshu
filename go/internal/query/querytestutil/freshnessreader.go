// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// FakeRepositoryFreshnessReader is the test double for
// repository.FreshnessReader. Promoted here for #6060 lane B3: the freshness
// route tests live in package repository while the graph-read-error sweep
// tests stay in package query, and an unexported double in either package
// is unreachable from the other. Fields are exported so both packages can
// build it with keyed literals; GotRepoID captures the repo the handler
// passed for assertion.
type FakeRepositoryFreshnessReader struct {
	Snapshot  status.RepositoryFreshnessSnapshot
	Err       error
	GotRepoID string
}

// ReadRepositoryFreshness implements the freshness-reader port against the
// installed Snapshot, returning Err when set.
func (f *FakeRepositoryFreshnessReader) ReadRepositoryFreshness(_ context.Context, repoID string) (status.RepositoryFreshnessSnapshot, error) {
	f.GotRepoID = repoID
	if f.Err != nil {
		return status.RepositoryFreshnessSnapshot{}, f.Err
	}
	return f.Snapshot, nil
}

// FullyBuiltRepositoryFreshnessSnapshot returns a resolved snapshot with
// generation, commit, and stages populated. Shared by package query and
// package repository freshness tests for the same reason as
// FakeRepositoryFreshnessReader: a builder in either package's _test.go is
// unreachable from the other.
func FullyBuiltRepositoryFreshnessSnapshot() status.RepositoryFreshnessSnapshot {
	activatedAt := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	return status.RepositoryFreshnessSnapshot{
		RepositoryID:  "repo-1",
		ScopeID:       "scope-1",
		Resolved:      true,
		ScopeKind:     "repository",
		HasGeneration: true,
		Generation: status.RepositoryFreshnessGeneration{
			ID: "gen-1", Status: "active", TriggerKind: "push", IsDelta: true, ActivatedAt: activatedAt,
		},
		ObservedCommit: "abc123",
		ObservedAt:     activatedAt.Add(-time.Minute),
		Stages:         status.RepositoryFreshnessStages{Collected: true, Reduced: true, Projected: true, Materialized: true},
	}
}

// TwoTenantChangedSinceScope is one ingestion_scopes row reduced to the three
// columns the freshness changed-since grant predicate reads: the scope id it
// matches AllowedScopeIDs against, the scope_kind that gates the repository
// arm, and the source_key a repository grant authorizes the scope through.
//
// Promoted here (#6642, #6608 rule) because GET /api/v0/freshness/changed-since's
// grant-boundary proof lives in package freshness
// (changed_since_two_tenant_test.go) while #6450's residual all-scope-bearer
// boundary proof lives in package query
// (auth_all_scope_bearer_two_tenant_test.go), and an unexported fixture in
// either package is unreachable from the other.
type TwoTenantChangedSinceScope struct {
	ScopeID   string
	ScopeKind string
	SourceKey string
}

// ChangedSinceTwoTenantPriorGeneration is the shared since_generation_id both
// the freshness and query two-tenant proofs request against
// GrantMirroringChangedSince's fixture rows.
const ChangedSinceTwoTenantPriorGeneration = "gen-prior"

// TwoTenantChangedSinceScopes returns the #5167 two-tenant fixture rows: two
// repository-kind scopes differing only by tenant, which is the harder case
// for a grant predicate than rows anchored on different kinds.
func TwoTenantChangedSinceScopes() []TwoTenantChangedSinceScope {
	return []TwoTenantChangedSinceScope{
		{ScopeID: "scope-a", ScopeKind: "repository", SourceKey: "repo-a"},
		{ScopeID: "scope-b", ScopeKind: "repository", SourceKey: "repo-b"},
	}
}

// GrantMirroringChangedSince is the #5167 two-tenant fixture for the
// changed-since delta route: the fake does not merely record the filter it
// was handed, it applies the SAME intersection resolveChangedSinceScopeQuery
// applies (go/internal/storage/postgres/changed_since_sql.go), so a handler
// that stops binding the caller grant resolves the other tenant's scope here
// exactly as it would in Postgres, and a caller's assertions fail.
//
//	$3::boolean = false                                             -> unbounded
//	scope.scope_kind = 'repository' AND scope.source_key = ANY($4)  -> repository grant
//	scope.scope_id = ANY($5)                                        -> scope grant
//
// Fields are exported so both package freshness and package query can build
// and inspect it with keyed literals and field access.
type GrantMirroringChangedSince struct {
	Scopes []TwoTenantChangedSinceScope
	// LastFilter is what the handler actually asked for. The empty-grant case
	// asserts against it because a status code alone cannot tell "the grant
	// was bound and matched nothing" from "the grant was never bound at all"
	// (#5167 review, round 2).
	LastFilter status.ChangedSinceFilter
	Called     bool
}

// ComputeChangedSinceDelta implements freshness.ChangedSinceReader.
func (g *GrantMirroringChangedSince) ComputeChangedSinceDelta(
	_ context.Context, filter status.ChangedSinceFilter,
) (status.ChangedSinceSummary, error) {
	g.LastFilter = filter
	g.Called = true

	for _, scope := range g.Scopes {
		// The selector arms of the shipped query: ($1 = '' OR scope_id = $1)
		// AND ($2 = '' OR (scope_kind = 'repository' AND source_key = $2)).
		if filter.ScopeID != "" && filter.ScopeID != scope.ScopeID {
			continue
		}
		if filter.Repository != "" &&
			(scope.ScopeKind != "repository" || scope.SourceKey != filter.Repository) {
			continue
		}
		if filter.Scoped && !changedSinceTwoTenantGrantAdmits(filter, scope) {
			continue
		}
		return status.ChangedSinceSummary{
			ScopeID:                   scope.ScopeID,
			ScopeKind:                 scope.ScopeKind,
			Repository:                scope.SourceKey,
			SinceGenerationID:         ChangedSinceTwoTenantPriorGeneration,
			CurrentActiveGenerationID: "gen-current-" + scope.SourceKey,
			SampleLimit:               filter.SampleLimit,
			Categories: []status.ChangedSinceCategoryDelta{{
				Category: status.ChangedSinceCategoryFiles,
				Counts:   status.ChangedSinceCounts{Added: 1},
			}},
		}, nil
	}
	// No row: the ungranted scope is indistinguishable from a missing one,
	// which is the whole point of binding the grant in the WHERE clause
	// instead of comparing strings in the handler.
	return status.ChangedSinceSummary{}, nil
}

// changedSinceTwoTenantGrantAdmits is the $3/$4/$5 arm of the shipped
// predicate: a repository-kind scope is admitted by source_key membership in
// the repository grant, and any scope is admitted by scope_id membership in
// the scope grant.
func changedSinceTwoTenantGrantAdmits(
	filter status.ChangedSinceFilter, scope TwoTenantChangedSinceScope,
) bool {
	if scope.ScopeKind == "repository" {
		for _, granted := range filter.AllowedRepositoryIDs {
			if granted == scope.SourceKey {
				return true
			}
		}
	}
	for _, granted := range filter.AllowedScopeIDs {
		if granted == scope.ScopeID {
			return true
		}
	}
	return false
}

// TwoTenantGenerationRow is one scope_generations row reduced to the four
// columns the freshness generation-lifecycle grant predicate reads.
//
// Promoted here for the same #6608 reason as TwoTenantChangedSinceScope.
type TwoTenantGenerationRow struct {
	GenerationID string
	ScopeID      string
	ScopeKind    string
	SourceKey    string
}

// TwoTenantGenerationRows returns the #5167 two-tenant fixture rows: two
// generations carrying the same shape and differing only by owning scope.
func TwoTenantGenerationRows() []TwoTenantGenerationRow {
	return []TwoTenantGenerationRow{
		{GenerationID: "gen-a", ScopeID: "scope-a", ScopeKind: "repository", SourceKey: "repo-a"},
		{GenerationID: "gen-b", ScopeID: "scope-b", ScopeKind: "repository", SourceKey: "repo-b"},
	}
}

// GrantMirroringGenerations is the #5167 two-tenant fixture for the
// generation lifecycle route: the fake applies the SAME intersection
// listGenerationLifecycleQuery applies (generation_lifecycle_sql.go), so a
// handler that stops binding the grant serves the other tenant's row and a
// caller's assertions fail.
//
//	$8 = false                                          -> unbounded
//	scope_kind = 'repository' AND source_key = ANY($9)  -> repository grant
//	generation.scope_id = ANY($10)                      -> scope grant
type GrantMirroringGenerations struct {
	Rows []TwoTenantGenerationRow
	// LastFilter is what the handler actually asked for. The shared-key and
	// empty-grant cases assert against it because a status code alone cannot
	// tell "the grant was bound and matched nothing" from "the grant was
	// never bound at all" (#5167 review, P2-1).
	LastFilter status.GenerationLifecycleFilter
	Called     bool
}

// ListGenerationLifecycle implements freshness.GenerationLifecycleReader.
func (g *GrantMirroringGenerations) ListGenerationLifecycle(
	_ context.Context, filter status.GenerationLifecycleFilter,
) (status.GenerationLifecyclePage, error) {
	g.LastFilter = filter
	g.Called = true

	var out []status.GenerationLifecycleRecord
	for _, row := range g.Rows {
		if filter.GenerationID != "" && filter.GenerationID != row.GenerationID {
			continue
		}
		if filter.Scoped && !generationTwoTenantGrantAdmits(filter, row) {
			continue
		}
		out = append(out, status.GenerationLifecycleRecord{
			ScopeID:      row.ScopeID,
			GenerationID: row.GenerationID,
			ScopeKind:    row.ScopeKind,
			Status:       "active",
		})
	}
	return status.GenerationLifecyclePage{Records: out, Limit: filter.Limit}, nil
}

func generationTwoTenantGrantAdmits(filter status.GenerationLifecycleFilter, row TwoTenantGenerationRow) bool {
	if row.ScopeKind == "repository" {
		for _, granted := range filter.AllowedRepositoryIDs {
			if granted == row.SourceKey {
				return true
			}
		}
	}
	for _, granted := range filter.AllowedScopeIDs {
		if granted == row.ScopeID {
			return true
		}
	}
	return false
}

// ScopedChangedSinceTenantA is the grant-bearing caller both the freshness
// changed-since/generations two-tenant proofs and #6450's residual
// all-scope-bearer boundary proof share: repo-a and scope-a, which
// TwoTenantChangedSinceScopes' and TwoTenantGenerationRows' first row carries
// and their second does not.
func ScopedChangedSinceTenantA() queryauth.AuthContext {
	return queryauth.AuthContext{
		Mode:                 queryauth.AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-a"},
		AllowedScopeIDs:      []string{"scope-a"},
	}
}

// DecodeChangedSinceEnvelope returns the changed-since response envelope's
// data map and error object so an assertion can name the field it depends on
// rather than matching a substring of the whole body.
func DecodeChangedSinceEnvelope(t *testing.T, rec *httptest.ResponseRecorder) (map[string]any, map[string]any) {
	t.Helper()

	var envelope struct {
		Data  map[string]any `json:"data"`
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response envelope: %v; body = %s", err, rec.Body.String())
	}
	return envelope.Data, envelope.Error
}
