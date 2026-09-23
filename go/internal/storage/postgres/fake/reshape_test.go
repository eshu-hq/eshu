// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fake_test

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
)

func eightColumnLegacyRow() []any {
	return []any{"scope", "generation", "fact-id", "attempt", "lease-owner", "created-at", "payload", "extra"}
}

func tenColumnLegacyRow() []any {
	return []any{
		"scope", "generation", "fact-id", "attempt", "lease-owner",
		int64(0), "created-at", "claimed-at", "cycle-started-at", "payload",
	}
}

func TestLegacyQueueRowAdapterPads8Into9WithZeroClaimEpoch(t *testing.T) {
	t.Parallel()

	row := eightColumnLegacyRow()
	original := append([]any(nil), row...)

	got := fake.LegacyQueueRowAdapter(9, row)

	if len(got) != 9 {
		t.Fatalf("len(got) = %d, want 9", len(got))
	}
	want := []any{
		"scope", "generation", "fact-id", "attempt", "lease-owner",
		int64(0), "created-at", "payload", "extra",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %v, want %v", i, got[i], want[i])
		}
	}
	// The caller's original row slice must be untouched.
	for i := range original {
		if row[i] != original[i] {
			t.Fatalf("adapter mutated the caller's row at index %d: got %v, want %v", i, row[i], original[i])
		}
	}
}

func TestLegacyQueueRowAdapterDuplicatesCycleStartedAtInto11ColumnRow(t *testing.T) {
	t.Parallel()

	row := tenColumnLegacyRow()
	original := append([]any(nil), row...)

	got := fake.LegacyQueueRowAdapter(11, row)

	if len(got) != 11 {
		t.Fatalf("len(got) = %d, want 11", len(got))
	}
	if got[8] != "cycle-started-at" || got[9] != "cycle-started-at" {
		t.Fatalf("got[8], got[9] = %v, %v, want cycle-started-at duplicated into both", got[8], got[9])
	}
	if got[10] != "payload" {
		t.Fatalf("got[10] = %v, want %v", got[10], "payload")
	}
	for i := range original {
		if row[i] != original[i] {
			t.Fatalf("adapter mutated the caller's row at index %d: got %v, want %v", i, row[i], original[i])
		}
	}
}

func TestLegacyQueueRowAdapterLeavesOtherShapesUntouched(t *testing.T) {
	t.Parallel()

	row := []any{"a", "b", "c"}
	got := fake.LegacyQueueRowAdapter(3, row)
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("got = %v, want the row unchanged", got)
	}

	// A genuine mismatch (not one of the two legacy shapes) passes through,
	// so the caller's own length check still catches it.
	mismatched := fake.LegacyQueueRowAdapter(4, row)
	if len(mismatched) != 3 {
		t.Fatalf("len(mismatched) = %d, want 3 (unchanged)", len(mismatched))
	}
}

func TestRowsScanAppliesItsOwnAdapterBeforeTheLengthCheck(t *testing.T) {
	t.Parallel()

	rows := &fake.Rows{
		Data:  [][]any{eightColumnLegacyRow()},
		Adapt: fake.LegacyQueueRowAdapter,
	}
	rows.Next()

	var (
		scope, generation, factID, attempt, leaseOwner, createdAt, payload, extra string
		claimEpoch                                                                int64
	)
	err := rows.Scan(&scope, &generation, &factID, &attempt, &leaseOwner, &claimEpoch, &createdAt, &payload, &extra)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if claimEpoch != 0 {
		t.Fatalf("claimEpoch = %d, want 0", claimEpoch)
	}
	if createdAt != "created-at" || payload != "payload" || extra != "extra" {
		t.Fatalf("createdAt, payload, extra = %q, %q, %q", createdAt, payload, extra)
	}
}

func TestRowsScanWithoutAdapterStillErrorsOnMismatch(t *testing.T) {
	t.Parallel()

	rows := &fake.Rows{Data: [][]any{eightColumnLegacyRow()}}
	rows.Next()
	dest := make([]any, 9)
	for i := range dest {
		var s string
		dest[i] = &s
	}
	if err := rows.Scan(dest...); err == nil {
		t.Fatal("Scan() error = nil, want a destination-count mismatch error with no adapter set")
	}
}

func TestExecQueryerAdaptAppliesToRowsHandedOutWhoseOwnAdaptIsNil(t *testing.T) {
	t.Parallel()

	database := &fake.ExecQueryer{
		Adapt:          fake.LegacyQueueRowAdapter,
		QueryResponses: []fake.Rows{{Data: [][]any{eightColumnLegacyRow()}}},
	}

	rows, err := database.QueryContext(context.Background(), "SELECT claims")
	if err != nil {
		t.Fatalf("QueryContext() error = %v, want nil", err)
	}
	if !rows.Next() {
		t.Fatal("Next() = false, want true")
	}
	var (
		scope, generation, factID, attempt, leaseOwner, createdAt, payload, extra string
		claimEpoch                                                                int64
	)
	err = rows.Scan(&scope, &generation, &factID, &attempt, &leaseOwner, &claimEpoch, &createdAt, &payload, &extra)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil (ExecQueryer.Adapt should apply)", err)
	}
}

func TestExecQueryerAdaptDoesNotOverrideRowsOwnAdapter(t *testing.T) {
	t.Parallel()

	called := false
	ownAdapter := fake.RowAdapter(func(destCount int, row []any) []any {
		called = true
		return row
	})
	database := &fake.ExecQueryer{
		Adapt: fake.LegacyQueueRowAdapter,
		QueryResponses: []fake.Rows{
			{Data: [][]any{{"only-column"}}, Adapt: ownAdapter},
		},
	}

	rows, err := database.QueryContext(context.Background(), "SELECT one")
	if err != nil {
		t.Fatalf("QueryContext() error = %v, want nil", err)
	}
	rows.Next()
	var dest string
	if err := rows.Scan(&dest); err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if !called {
		t.Fatal("Rows' own Adapt was not used; ExecQueryer.Adapt overrode it")
	}
}
