// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7126 disposable PostgreSQL 18 proof of the config-artifact backfill marker: absent after bootstrap, present after one record, stable across a second record from a second writer; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/config/artifacts"
)

// TestBackfillMarkerRoundTripLive proves the marker the walker gates PR3c
// readiness on: no marker row after bootstrap, BackfillComplete false;
// RecordBackfillMarker inserts it, BackfillComplete true; a second record
// (the other replica finishing the same walk) keeps it true without error.
func TestBackfillMarkerRoundTripLive(t *testing.T) {
	ctx, db := openDatabase(t)

	complete, err := artifacts.BackfillComplete(ctx, postgres.SQLDB{DB: db})
	if err != nil {
		t.Fatalf("BackfillComplete before record = %v, want no error", err)
	}
	if complete {
		t.Fatal("BackfillComplete before record = true, want false on a fresh bootstrap")
	}
	if err := artifacts.RecordBackfillMarker(ctx, postgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("RecordBackfillMarker = %v, want no error", err)
	}
	complete, err = artifacts.BackfillComplete(ctx, postgres.SQLDB{DB: db})
	if err != nil {
		t.Fatalf("BackfillComplete after record = %v, want no error", err)
	}
	if !complete {
		t.Fatal("BackfillComplete after record = false, want true")
	}
	if err := artifacts.RecordBackfillMarker(ctx, postgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("second RecordBackfillMarker = %v, want no error (ON CONFLICT DO NOTHING)", err)
	}
	complete, err = artifacts.BackfillComplete(ctx, postgres.SQLDB{DB: db})
	if err != nil || !complete {
		t.Fatalf("BackfillComplete after second record = (%v, %v), want (true, nil)", complete, err)
	}
}
