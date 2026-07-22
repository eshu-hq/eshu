// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestTerraformConfigStateDriftFilterToPostgresThreadsScopeGrantFields is the
// #5442 P2 regression: the query-layer-to-postgres filter mapping must carry
// Scoped/AllowedScopeIDs so the SQL layer also enforces the caller's grant
// (defense-in-depth), not only ScopeID/Address/Outcome/DriftKinds/Limit/
// Offset.
func TestTerraformConfigStateDriftFilterToPostgresThreadsScopeGrantFields(t *testing.T) {
	t.Parallel()

	filter := TerraformConfigStateDriftFindingFilter{
		ScopeID:         "state_snapshot:s3:hash-1",
		Address:         "aws_s3_bucket.x",
		Outcome:         "ambiguous",
		DriftKinds:      []string{"added_in_state"},
		Limit:           25,
		Offset:          5,
		Scoped:          true,
		AllowedScopeIDs: []string{"repo-a", "state_snapshot:s3:hash-1"},
	}
	got := terraformConfigStateDriftFilterToPostgres(filter)
	want := postgres.TerraformConfigStateDriftFindingFilter{
		ScopeID:         "state_snapshot:s3:hash-1",
		Address:         "aws_s3_bucket.x",
		Outcome:         "ambiguous",
		DriftKinds:      []string{"added_in_state"},
		Limit:           25,
		Offset:          5,
		Scoped:          true,
		AllowedScopeIDs: []string{"repo-a", "state_snapshot:s3:hash-1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("terraformConfigStateDriftFilterToPostgres() = %#v, want %#v", got, want)
	}
}

// TestBindTerraformConfigStateDriftFilterAccessSetsScopeGrant proves the
// handler-side binder (the #5442 P2 companion to the P1 access precheck)
// populates Scoped/AllowedScopeIDs from the caller's merged repository/scope
// grant for a scoped caller, and leaves the filter unscoped for an
// all-scopes caller.
func TestBindTerraformConfigStateDriftFilterAccessSetsScopeGrant(t *testing.T) {
	t.Parallel()

	scopedAccess := repositoryAccessFilterFromContext(ContextWithAuthContext(context.Background(), AuthContext{
		Mode:                 AuthModeScoped,
		AllowedRepositoryIDs: []string{"state_snapshot:s3:hash-1", "repo-a"},
	}))
	filter := bindTerraformConfigStateDriftFilterAccess(scopedAccess, TerraformConfigStateDriftFindingFilter{ScopeID: "state_snapshot:s3:hash-1"})
	if !filter.Scoped {
		t.Fatal("filter.Scoped = false, want true for a scoped caller")
	}
	want := []string{"repo-a", "state_snapshot:s3:hash-1"}
	if !reflect.DeepEqual(filter.AllowedScopeIDs, want) {
		t.Fatalf("filter.AllowedScopeIDs = %#v, want %#v", filter.AllowedScopeIDs, want)
	}

	unscopedAccess := repositoryAccessFilterFromContext(context.Background())
	unscopedFilter := bindTerraformConfigStateDriftFilterAccess(unscopedAccess, TerraformConfigStateDriftFindingFilter{ScopeID: "state_snapshot:s3:hash-1"})
	if unscopedFilter.Scoped {
		t.Fatal("unscopedFilter.Scoped = true, want false for an all-scopes caller")
	}
	if unscopedFilter.AllowedScopeIDs != nil {
		t.Fatalf("unscopedFilter.AllowedScopeIDs = %#v, want nil for an all-scopes caller", unscopedFilter.AllowedScopeIDs)
	}
}
