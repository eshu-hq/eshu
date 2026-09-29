// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestSharedIntentUpsertCreatedAtLastWriterWinsLive pins the #7323 ruling on the
// shared-projection intent upsert: re-upserting an existing intent overwrites
// created_at and payload (last writer wins) while completed_at only advances
// (COALESCE keeps the original on a completed row, and a pending row takes the
// supplied value). created_at is the acceptance epoch of the RUNS_ON readiness
// fence, so this is intended; see upsertSharedIntentBatchSuffix.
//
// It runs in the reducer contention gate against the bootstrapped schema built
// by openAcceptanceMonotonicFixture; an unset DSN there is a failure, not a skip.
func TestSharedIntentUpsertCreatedAtLastWriterWinsLive(t *testing.T) {
	fixture := openAcceptanceMonotonicFixture(t)
	ctx := t.Context()
	db := fixture.db
	store := NewSharedIntentStore(SQLDB{DB: db})

	const (
		scope       = "upsert-proof-7323:scope"
		repo        = "upsert-proof-7323:repo"
		run         = "upsert-proof-7323:run"
		gen         = "upsert-proof-7323:gen"
		completedID = "upsert-proof-7323-completed"
		pendingID   = "upsert-proof-7323-pending"
	)

	// TIMESTAMPTZ keeps microseconds; whole seconds survive the round trip.
	oldEpoch := time.Date(2026, time.September, 20, 9, 0, 0, 0, time.UTC)
	newEpoch := oldEpoch.Add(time.Hour)
	originalCompletedAt := oldEpoch.Add(time.Minute)
	laterCompletedAt := newEpoch.Add(time.Minute)

	row := func(intentID string, createdAt time.Time, completedAt *time.Time, marker string) reducer.SharedProjectionIntentRow {
		return reducer.SharedProjectionIntentRow{
			IntentID:         intentID,
			ProjectionDomain: reducer.DomainRepoDependency,
			PartitionKey:     intentID,
			ScopeID:          scope,
			AcceptanceUnitID: repo,
			RepositoryID:     repo,
			SourceRunID:      run,
			GenerationID:     gen,
			Payload:          map[string]any{"repo_id": repo, "marker": marker},
			CreatedAt:        createdAt,
			CompletedAt:      completedAt,
		}
	}
	upsert := func(rows ...reducer.SharedProjectionIntentRow) {
		t.Helper()
		if err := store.UpsertIntents(ctx, rows); err != nil {
			t.Fatalf("upsert intents: %v", err)
		}
	}
	read := func(intentID string) (createdAt time.Time, completedAt sql.NullTime, marker string) {
		t.Helper()
		var payload []byte
		if err := db.QueryRowContext(ctx, `
SELECT created_at, completed_at, payload
FROM shared_projection_intents WHERE intent_id = $1`, intentID).Scan(&createdAt, &completedAt, &payload); err != nil {
			t.Fatalf("read %s: %v", intentID, err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(payload, &decoded); err != nil {
			t.Fatalf("decode payload of %s: %v", intentID, err)
		}
		marker, _ = decoded["marker"].(string)
		return createdAt.UTC(), completedAt, marker
	}

	// First writes: one completed intent, one pending intent.
	upsert(
		row(completedID, oldEpoch, &originalCompletedAt, "first"),
		row(pendingID, oldEpoch, nil, "first"),
	)
	// A retry re-emits both at a newer epoch; the completed one carries a later
	// completed_at, the pending one supplies a completed_at for the first time.
	upsert(
		row(completedID, newEpoch, &laterCompletedAt, "second"),
		row(pendingID, newEpoch, &laterCompletedAt, "second"),
	)

	createdAt, completedAt, marker := read(completedID)
	if !createdAt.Equal(newEpoch) {
		t.Fatalf("completed row created_at = %s, want overwritten to %s", createdAt, newEpoch)
	}
	if marker != "second" {
		t.Fatalf("completed row payload marker = %q, want overwritten to %q", marker, "second")
	}
	if !completedAt.Valid || !completedAt.Time.Equal(originalCompletedAt) {
		t.Fatalf("completed row completed_at = %v, want original %s kept (advance-only)", completedAt, originalCompletedAt)
	}

	createdAt, completedAt, marker = read(pendingID)
	if !createdAt.Equal(newEpoch) || marker != "second" {
		t.Fatalf("pending row = (created_at %s, marker %q), want overwritten to (%s, second)", createdAt, marker, newEpoch)
	}
	if !completedAt.Valid || !completedAt.Time.Equal(laterCompletedAt) {
		t.Fatalf("pending row completed_at = %v, want EXCLUDED value %s via COALESCE", completedAt, laterCompletedAt)
	}

	// A retry that supplies no completed_at must not reopen a completed row.
	upsert(row(completedID, newEpoch.Add(time.Hour), nil, "third"))
	createdAt, completedAt, marker = read(completedID)
	if !createdAt.Equal(newEpoch.Add(time.Hour)) || marker != "third" {
		t.Fatalf("completed row = (created_at %s, marker %q) after nil-completed retry, want overwritten", createdAt, marker)
	}
	if !completedAt.Valid || !completedAt.Time.Equal(originalCompletedAt) {
		t.Fatalf("completed row completed_at = %v after nil-completed retry, want original %s (never reopened)", completedAt, originalCompletedAt)
	}
}
