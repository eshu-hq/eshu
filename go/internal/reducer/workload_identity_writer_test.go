package reducer

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestPostgresWorkloadIdentityWriterPersistsCanonicalFact(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC)
	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresWorkloadIdentityWriter{
		DB:  db,
		Now: func() time.Time { return now },
	}

	result, err := writer.WriteWorkloadIdentity(context.Background(), WorkloadIdentityWrite{
		IntentID:        "intent-1",
		ScopeID:         "scope-123",
		GenerationID:    "generation-456",
		SourceSystem:    "git",
		Cause:           "shared follow-up",
		EntityKeys:      []string{"repo-b", "repo-a"},
		RelatedScopeIDs: []string{"scope-999", "scope-123"},
	})
	if err != nil {
		t.Fatalf("WriteWorkloadIdentity() error = %v, want nil", err)
	}

	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("WriteWorkloadIdentity().CanonicalWrites = %d, want %d", got, want)
	}
	if got := result.CanonicalID; got == "" {
		t.Fatal("WriteWorkloadIdentity().CanonicalID = empty, want non-empty")
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

func TestPostgresWorkloadIdentityWriterRequiresDatabase(t *testing.T) {
	t.Parallel()

	_, err := PostgresWorkloadIdentityWriter{}.WriteWorkloadIdentity(
		context.Background(),
		WorkloadIdentityWrite{IntentID: "intent-1"},
	)
	if err == nil {
		t.Fatal("WriteWorkloadIdentity() error = nil, want non-nil")
	}
}

type fakeWorkloadIdentityExecer struct {
	execs []fakeWorkloadIdentityExecCall
}

type fakeWorkloadIdentityExecCall struct {
	query string
	args  []any
}

func (f *fakeWorkloadIdentityExecer) ExecContext(
	_ context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	f.execs = append(f.execs, fakeWorkloadIdentityExecCall{query: query, args: args})
	return fakeWorkloadIdentityResult{}, nil
}

type fakeWorkloadIdentityResult struct{}

func (fakeWorkloadIdentityResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeWorkloadIdentityResult) RowsAffected() (int64, error) { return 1, nil }
