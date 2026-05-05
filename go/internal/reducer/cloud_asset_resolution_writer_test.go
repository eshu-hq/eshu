package reducer

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
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
	return fakeCloudAssetResolutionResult{}, nil
}

type fakeCloudAssetResolutionResult struct{}

func (fakeCloudAssetResolutionResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeCloudAssetResolutionResult) RowsAffected() (int64, error) { return 1, nil }
