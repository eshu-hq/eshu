// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudasset

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
)

func TestPostgresCloudAssetResolutionWriterPersistsCanonicalFact(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC)
	db := &fakeCloudAssetResolutionExecer{}
	writer := PostgresCloudAssetResolutionWriter{
		DB:  db,
		Now: func() time.Time { return now },
	}

	result, err := writer.WriteCloudAssetResolution(context.Background(), CloudAssetResolutionWrite{
		IntentID:        "intent-1",
		ScopeID:         "scope-123",
		GenerationID:    "generation-456",
		SourceSystem:    "git",
		Cause:           "shared follow-up",
		EntityKeys:      []string{"aws:s3:bucket:logs-prod", "tfstate:logs-prod"},
		RelatedScopeIDs: []string{"scope-999", "scope-123"},
	})
	if err != nil {
		t.Fatalf("WriteCloudAssetResolution() error = %v, want nil", err)
	}

	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("WriteCloudAssetResolution().CanonicalWrites = %d, want %d", got, want)
	}
	if got := result.CanonicalID; got == "" {
		t.Fatal("WriteCloudAssetResolution().CanonicalID = empty, want non-empty")
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("ExecContext calls = %d, want %d", got, want)
	}
	if got := db.execs[0].query; !strings.Contains(got, "INSERT INTO fact_records") {
		t.Fatalf("ExecContext query = %q, want fact_records insert", got)
	}
	if got, want := db.execs[0].args[0], "intent-1"; got != want {
		t.Fatalf("ExecContext fact_id = %v, want %v", got, want)
	}
	if got, want := len(db.execs[0].args), 15; got != want {
		t.Fatalf("ExecContext arg count = %d, want %d", got, want)
	}
	if got, want := db.execs[0].args[5], "git"; got != want {
		t.Fatalf("ExecContext collector_kind = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[6], facts.SourceConfidenceInferred; got != want {
		t.Fatalf("ExecContext source_confidence = %v, want %v", got, want)
	}
}

func TestPostgresCloudAssetResolutionWriterRequiresDatabase(t *testing.T) {
	t.Parallel()

	_, err := PostgresCloudAssetResolutionWriter{}.WriteCloudAssetResolution(
		context.Background(),
		CloudAssetResolutionWrite{IntentID: "intent-1"},
	)
	if err == nil {
		t.Fatal("WriteCloudAssetResolution() error = nil, want non-nil")
	}
}

type fakeCloudAssetResolutionExecer struct {
	execs []fakeCloudAssetResolutionExecCall
	// skipped makes the statement report zero rows affected, as the scope owner
	// leg does when another scope holds the fact id (#11213).
	skipped bool
}

type fakeCloudAssetResolutionExecCall struct {
	query string
	args  []any
}

func (f *fakeCloudAssetResolutionExecer) ExecContext(
	_ context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	f.execs = append(f.execs, fakeCloudAssetResolutionExecCall{query: query, args: args})
	if f.skipped {
		return fakeCloudAssetResolutionSkippedResult{}, nil
	}
	return fakeCloudAssetResolutionResult{}, nil
}

type fakeCloudAssetResolutionSkippedResult struct{}

func (fakeCloudAssetResolutionSkippedResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeCloudAssetResolutionSkippedResult) RowsAffected() (int64, error) { return 0, nil }

type fakeCloudAssetResolutionResult struct{}

func (fakeCloudAssetResolutionResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeCloudAssetResolutionResult) RowsAffected() (int64, error) { return 1, nil }

// TestPostgresCloudAssetResolutionWriterRefusesAFactIDAnotherScopeHolds pins the
// #11213 contract: the intent id is the fact id here, so a row the scope owner
// leg skipped is a typed, non-retryable owner conflict, never a silent success.
func TestPostgresCloudAssetResolutionWriterRefusesAFactIDAnotherScopeHolds(t *testing.T) {
	t.Parallel()

	writer := PostgresCloudAssetResolutionWriter{DB: &fakeCloudAssetResolutionExecer{skipped: true}}
	_, err := writer.WriteCloudAssetResolution(context.Background(), CloudAssetResolutionWrite{
		IntentID: "intent-1", ScopeID: "scope-123", GenerationID: "generation-456", SourceSystem: "git",
	})
	var conflict *factwrite.OwnerConflictError
	if !errors.As(err, &conflict) || conflict.Writer != factwrite.WriterSingle || conflict.Retryable() {
		t.Fatalf("WriteCloudAssetResolution() error = %v, want a non-retryable single-writer owner conflict", err)
	}
}
