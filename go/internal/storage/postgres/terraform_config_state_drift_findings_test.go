// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
)

func TestTerraformConfigStateDriftFindingStoreListsActiveScopedFindings(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{
				"fact:terraform-drift-1",
				"state_snapshot:s3:hash-1",
				"generation:tf-1",
				"collector/terraform-state",
				observedAt,
				[]byte(`{
					"canonical_id":"canonical:x",
					"candidate_id":"drift:hash-1:aws_s3_bucket.x:added_in_state",
					"candidate_kind":"terraform_config_state_drift",
					"outcome":"exact",
					"address":"aws_s3_bucket.x",
					"drift_kind":"added_in_state",
					"backend_kind":"s3",
					"locator_hash":"hash-1",
					"confidence":1,
					"evidence":[{
						"id":"evidence:state",
						"source_system":"reducer/terraform_config_state_drift",
						"evidence_type":"terraform_drift_address",
						"scope_id":"state_snapshot:s3:hash-1",
						"key":"resource_address",
						"value":"aws_s3_bucket.x",
						"confidence":1
					}]
				}`),
			}}},
		},
	}
	store := NewTerraformConfigStateDriftFindingStore(db)

	rows, err := store.ListActiveFindings(context.Background(), TerraformConfigStateDriftFindingFilter{
		ScopeID: "state_snapshot:s3:hash-1",
		Limit:   25,
	})
	if err != nil {
		t.Fatalf("ListActiveFindings() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	row := rows[0]
	if got, want := row.Address, "aws_s3_bucket.x"; got != want {
		t.Fatalf("row.Address = %q, want %q", got, want)
	}
	if got, want := row.Outcome, "exact"; got != want {
		t.Fatalf("row.Outcome = %q, want %q", got, want)
	}
	if got, want := row.DriftKind, "added_in_state"; got != want {
		t.Fatalf("row.DriftKind = %q, want %q", got, want)
	}
	if got, want := len(row.Evidence), 1; got != want {
		t.Fatalf("len(row.Evidence) = %d, want %d", got, want)
	}
}

func TestTerraformConfigStateDriftFindingStoreCountsActiveScopedFindings(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{{3}}}},
	}
	store := NewTerraformConfigStateDriftFindingStore(db)

	count, err := store.CountActiveFindings(context.Background(), TerraformConfigStateDriftFindingFilter{
		ScopeID: "state_snapshot:s3:hash-1",
	})
	if err != nil {
		t.Fatalf("CountActiveFindings() error = %v, want nil", err)
	}
	if got, want := count, 3; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
}

func TestTerraformConfigStateDriftFindingStoreFiltersByOutcome(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{}}}
	store := NewTerraformConfigStateDriftFindingStore(db)

	_, err := store.ListActiveFindings(context.Background(), TerraformConfigStateDriftFindingFilter{
		ScopeID: "state_snapshot:s3:hash-1",
		Outcome: "ambiguous",
	})
	if err != nil {
		t.Fatalf("ListActiveFindings() error = %v, want nil", err)
	}
	query := db.queries[0].query
	if !strings.Contains(query, "fact.payload->>'outcome' = $3") {
		t.Fatalf("query missing outcome predicate: %s", query)
	}
	if got, want := db.queries[0].args[2], "ambiguous"; got != want {
		t.Fatalf("outcome arg = %#v, want %#v", got, want)
	}
}

// TestTerraformConfigStateDriftFindingStoreRejectsUnboundedFilters proves
// ScopeID is mandatory (no account-wide fan-out exists for this domain,
// unlike AWS's account_id fallback) and that a non-state_snapshot scope_id is
// rejected before any query is issued.
func TestTerraformConfigStateDriftFindingStoreRejectsUnboundedFilters(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewTerraformConfigStateDriftFindingStore(db)

	for _, tc := range []struct {
		name   string
		filter TerraformConfigStateDriftFindingFilter
	}{
		{name: "missing scope", filter: TerraformConfigStateDriftFindingFilter{}},
		{name: "non state_snapshot scope", filter: TerraformConfigStateDriftFindingFilter{ScopeID: "repo:repo-1@abc"}},
		{name: "invalid outcome", filter: TerraformConfigStateDriftFindingFilter{ScopeID: "state_snapshot:s3:hash-1", Outcome: "stale"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := store.ListActiveFindings(context.Background(), tc.filter); err == nil {
				t.Fatal("ListActiveFindings() error = nil, want bounded filter error")
			}
			if _, err := store.CountActiveFindings(context.Background(), tc.filter); err == nil {
				t.Fatal("CountActiveFindings() error = nil, want bounded filter error")
			}
		})
	}
	if got := len(db.queries); got != 0 {
		t.Fatalf("query count = %d, want 0", got)
	}
}

func TestTerraformConfigStateDriftFindingStoreCapsLimit(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{}}}
	store := NewTerraformConfigStateDriftFindingStore(db)

	_, err := store.ListActiveFindings(context.Background(), TerraformConfigStateDriftFindingFilter{
		ScopeID: "state_snapshot:s3:hash-1",
		Limit:   5000,
	})
	if err != nil {
		t.Fatalf("ListActiveFindings() error = %v, want nil", err)
	}
	if got, want := db.queries[0].args[2], 500; got != want {
		t.Fatalf("limit arg = %#v, want %#v", got, want)
	}
}

// TestTerraformConfigStateDriftFindingStoreScopedGrantBindsScopePredicate is
// the direct-SQL proof that the store's tenant-isolation mechanism is the
// emitted query, mirroring the AWS drift finding store's #5167 W4 test.
func TestTerraformConfigStateDriftFindingStoreScopedGrantBindsScopePredicate(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{{0}}}}}
	store := NewTerraformConfigStateDriftFindingStore(db)

	grant := []string{"state_snapshot:s3:hash-1"}
	_, err := store.CountActiveFindings(context.Background(), TerraformConfigStateDriftFindingFilter{
		ScopeID:         "state_snapshot:s3:hash-1",
		Scoped:          true,
		AllowedScopeIDs: grant,
	})
	if err != nil {
		t.Fatalf("CountActiveFindings() error = %v, want nil", err)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	query := db.queries[0].query
	if !strings.Contains(query, "AND fact.scope_id = ANY($3)") {
		t.Fatalf("query missing AND-combined scope grant predicate at $3: %s", query)
	}
	if strings.Contains(query, " OR ") {
		t.Fatalf("query must not OR-combine the scope grant predicate: %s", query)
	}
	if got, want := db.queries[0].args[2], pq.StringArray(grant); !reflect.DeepEqual(got, want) {
		t.Fatalf("scope grant arg = %#v, want %#v", got, want)
	}
}

// TestTerraformConfigStateDriftFindingStoreEmptyScopedGrantSkipsQuery proves
// a scoped caller with no granted state-snapshot scope gets zero rows without
// any query ever being issued -- an honest empty page, not a leak.
func TestTerraformConfigStateDriftFindingStoreEmptyScopedGrantSkipsQuery(t *testing.T) {
	t.Parallel()

	filter := TerraformConfigStateDriftFindingFilter{
		ScopeID:         "state_snapshot:s3:hash-1",
		Scoped:          true,
		AllowedScopeIDs: nil,
	}

	t.Run("list", func(t *testing.T) {
		t.Parallel()
		db := &fakeExecQueryer{}
		store := NewTerraformConfigStateDriftFindingStore(db)
		rows, err := store.ListActiveFindings(context.Background(), filter)
		if err != nil {
			t.Fatalf("ListActiveFindings() error = %v, want nil", err)
		}
		if len(rows) != 0 {
			t.Fatalf("rows = %#v, want empty for an empty scoped grant", rows)
		}
		if got := len(db.queries); got != 0 {
			t.Fatalf("query count = %d, want 0 -- an empty scoped grant must issue no query", got)
		}
	})

	t.Run("count", func(t *testing.T) {
		t.Parallel()
		db := &fakeExecQueryer{}
		store := NewTerraformConfigStateDriftFindingStore(db)
		count, err := store.CountActiveFindings(context.Background(), filter)
		if err != nil {
			t.Fatalf("CountActiveFindings() error = %v, want nil", err)
		}
		if count != 0 {
			t.Fatalf("count = %d, want 0", count)
		}
		if got := len(db.queries); got != 0 {
			t.Fatalf("query count = %d, want 0 -- an empty scoped grant must issue no query", got)
		}
	})
}
