// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "context"

// fakeIaCManagementStore is this package's own copy of iac/management_test.go's
// identically named fixture (#6642 Part A). Both copies exist because several
// staying root tests (aws_runtime_drift_test.go,
// replatforming_ownership_handler_test.go, replatforming_plan_handler_test.go,
// replatforming_plan_waves_handler_test.go, replatforming_rollups_handler_test.go)
// construct an IaCHandler through the
// root alias and need something satisfying IaCManagementStore, but the
// canonical fixture moved to package iac with its primary callers
// (TestHandleUnmanagedCloudResources*, TestHandleIaCManagementStatus*,
// TestHandleIaCManagementExplanation*) and a root test cannot reach an
// unexported type declared in a different package. Keep both copies in sync
// by hand until a shared querytestutil double exists for this contract.
type fakeIaCManagementStore struct {
	rows           []IaCManagementFindingRow
	observedFilter *IaCManagementFilter
	// dbTouched, when non-nil, is set true the first time either method reads
	// f.rows -- i.e. would have issued a real Postgres query. It stays false
	// for a scoped call with an empty AllowedScopeIDs grant.
	dbTouched *bool
}

// scopedRows applies filter.ARN (when set) alongside the Scoped grant
// intersection -- the same two predicates buildAWSCloudRuntimeDriftFindingQuery
// combines in its WHERE clause (fact.payload->>'arn' = $arn AND
// fact.scope_id = ANY($allowed)) -- so an exact-lookup request naming an
// out-of-grant ARN correctly resolves to zero rows here too, not just an
// in-grant ARN that happens to be first in f.rows. Both extra predicates are
// gated on filter.Scoped so every pre-#5167-W4 test that never sets Scoped
// keeps its original unfiltered-by-request-fields behavior (those tests only
// ever rely on Offset/Limit slicing, not ARN/scope_id/account_id filtering).
func (f fakeIaCManagementStore) scopedRows(filter IaCManagementFilter) []IaCManagementFindingRow {
	if !filter.Scoped {
		return f.rows
	}
	if len(filter.AllowedScopeIDs) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(filter.AllowedScopeIDs))
	for _, scopeID := range filter.AllowedScopeIDs {
		allowed[scopeID] = struct{}{}
	}
	filtered := make([]IaCManagementFindingRow, 0, len(f.rows))
	for _, row := range f.rows {
		if _, ok := allowed[row.ScopeID]; !ok {
			continue
		}
		if filter.ARN != "" && row.ARN != filter.ARN {
			continue
		}
		filtered = append(filtered, row)
	}
	return filtered
}

func (f fakeIaCManagementStore) touchDB() {
	if f.dbTouched != nil {
		*f.dbTouched = true
	}
}

func (f fakeIaCManagementStore) ListUnmanagedCloudResources(
	_ context.Context,
	filter IaCManagementFilter,
) ([]IaCManagementFindingRow, error) {
	if f.observedFilter != nil {
		*f.observedFilter = filter
	}
	if filter.Scoped && len(filter.AllowedScopeIDs) == 0 {
		return nil, nil
	}
	f.touchDB()
	rows := append([]IaCManagementFindingRow(nil), f.scopedRows(filter)...)
	if filter.Offset > len(rows) {
		return nil, nil
	}
	rows = rows[filter.Offset:]
	if filter.Limit > 0 && len(rows) > filter.Limit {
		rows = rows[:filter.Limit]
	}
	return rows, nil
}

func (f fakeIaCManagementStore) CountUnmanagedCloudResources(_ context.Context, filter IaCManagementFilter) (int, error) {
	if filter.Scoped && len(filter.AllowedScopeIDs) == 0 {
		return 0, nil
	}
	f.touchDB()
	return len(f.scopedRows(filter)), nil
}
