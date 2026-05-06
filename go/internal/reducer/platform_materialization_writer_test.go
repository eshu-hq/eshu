package reducer

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

func TestPostgresPlatformMaterializationWriterPersistsCanonicalFact(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC)
	db := &fakePlatformMaterializationExecer{}
	writer := PostgresPlatformMaterializationWriter{
		DB:  db,
		Now: func() time.Time { return now },
	}

	result, err := writer.WritePlatformMaterialization(context.Background(), PlatformMaterializationWrite{
		IntentID:        "intent-1",
		ScopeID:         "scope-123",
		GenerationID:    "generation-456",
		SourceSystem:    "git",
		Cause:           "platform materialization follow-up",
		EntityKeys:      []string{"entity-b", "entity-a"},
		RelatedScopeIDs: []string{"scope-999", "scope-123"},
	})
	if err != nil {
		t.Fatalf("WritePlatformMaterialization() error = %v, want nil", err)
	}

	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("result.CanonicalWrites = %d, want %d", got, want)
	}
	if got := result.CanonicalID; got == "" {
		t.Fatal("result.CanonicalID = empty, want non-empty")
	}
	if !strings.HasPrefix(result.CanonicalID, "canonical:") {
		t.Fatalf("result.CanonicalID = %q, want canonical: prefix", result.CanonicalID)
	}
	if got := result.EvidenceSummary; got == "" {
		t.Fatal("result.EvidenceSummary = empty, want non-empty")
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
	if got, want := db.execs[0].args[3], "reducer_platform_materialization"; got != want {
		t.Fatalf("ExecContext fact_kind = %v, want %v", got, want)
	}
}

func TestPostgresPlatformMaterializationWriterRequiresDatabase(t *testing.T) {
	t.Parallel()

	_, err := PostgresPlatformMaterializationWriter{}.WritePlatformMaterialization(
		context.Background(),
		PlatformMaterializationWrite{IntentID: "intent-1"},
	)
	if err == nil {
		t.Fatal("WritePlatformMaterialization() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "database is required") {
		t.Fatalf("error = %q, want 'database is required'", err.Error())
	}
}

func TestPostgresPlatformMaterializationWriterStableFactKeyDeterministic(t *testing.T) {
	t.Parallel()

	write := PlatformMaterializationWrite{
		ScopeID:         "scope-1",
		GenerationID:    "gen-1",
		EntityKeys:      []string{"b", "a", "c"},
		RelatedScopeIDs: []string{"scope-2", "scope-1"},
	}

	key1 := platformMaterializationStableFactKey(write)
	key2 := platformMaterializationStableFactKey(write)
	if key1 != key2 {
		t.Fatalf("stable fact key not deterministic: %q != %q", key1, key2)
	}
	if !strings.Contains(key1, "platform_materialization") {
		t.Fatalf("stable fact key missing domain: %q", key1)
	}
}

func TestPostgresPlatformMaterializationWriterCanonicalIDDeterministic(t *testing.T) {
	t.Parallel()

	write := PlatformMaterializationWrite{
		ScopeID:         "scope-1",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		EntityKeys:      []string{"a", "b"},
		RelatedScopeIDs: []string{"scope-1"},
	}

	id1 := canonicalPlatformMaterializationID(write)
	id2 := canonicalPlatformMaterializationID(write)
	if id1 != id2 {
		t.Fatalf("canonical ID not deterministic: %q != %q", id1, id2)
	}
	if !strings.HasPrefix(id1, "canonical:") {
		t.Fatalf("canonical ID missing prefix: %q", id1)
	}
}

type fakePlatformMaterializationExecer struct {
	execs []fakePlatformMaterializationExecCall
}

type fakePlatformMaterializationExecCall struct {
	query string
	args  []any
}

func (f *fakePlatformMaterializationExecer) ExecContext(
	_ context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	f.execs = append(f.execs, fakePlatformMaterializationExecCall{query: query, args: args})
	return fakePlatformMaterializationResult{}, nil
}

type fakePlatformMaterializationResult struct{}

func (fakePlatformMaterializationResult) LastInsertId() (int64, error) { return 0, nil }
func (fakePlatformMaterializationResult) RowsAffected() (int64, error) { return 1, nil }
