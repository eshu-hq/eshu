package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

type recordingCollectorEvidenceExecer struct {
	queries []string
	args    [][]any
	err     error
}

func (e *recordingCollectorEvidenceExecer) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	e.queries = append(e.queries, query)
	e.args = append(e.args, args)
	if e.err != nil {
		return nil, e.err
	}
	return result{}, nil
}

func TestRebuildAllCollectorEvidenceRequiresDB(t *testing.T) {
	t.Parallel()

	var store CollectorEvidenceSummaryStore
	if err := store.RebuildAllCollectorEvidence(context.Background(), time.Now()); err == nil {
		t.Fatal("RebuildAllCollectorEvidence() with nil DB error = nil, want non-nil")
	}
}

func TestRebuildAllCollectorEvidenceExecutesAtomicResweep(t *testing.T) {
	t.Parallel()

	exec := &recordingCollectorEvidenceExecer{}
	store := NewCollectorEvidenceSummaryStore(exec)
	at := time.Date(2026, 6, 21, 9, 0, 0, 0, time.UTC)
	if err := store.RebuildAllCollectorEvidence(context.Background(), at); err != nil {
		t.Fatalf("RebuildAllCollectorEvidence() error = %v, want nil", err)
	}
	if len(exec.queries) != 1 {
		t.Fatalf("resweep executed %d statements, want exactly 1 (atomic)", len(exec.queries))
	}
	if len(exec.args[0]) != 1 || exec.args[0][0] != at {
		t.Fatalf("resweep args = %#v, want [%v] (materialized_at watermark)", exec.args[0], at)
	}
}

func TestRebuildAllCollectorEvidenceWrapsExecError(t *testing.T) {
	t.Parallel()

	exec := &recordingCollectorEvidenceExecer{err: errors.New("boom")}
	store := NewCollectorEvidenceSummaryStore(exec)
	if err := store.RebuildAllCollectorEvidence(context.Background(), time.Now()); err == nil {
		t.Fatal("RebuildAllCollectorEvidence() error = nil, want wrapped exec error")
	}
}

// TestRebuildCollectorEvidenceSQLIsAtomicUpsertDeleteStale guards the resweep
// shape: one statement that recomputes the active per-scope aggregate, upserts
// every active summary row, and deletes summary rows no longer in the active set
// (covers superseded generations, tombstones, and FK-cascade pruned scopes). The
// per-scope LATERAL aggregate must be byte-equivalent to the pre-#3466 readiness
// aggregate so observation_count stays exact.
func TestRebuildCollectorEvidenceSQLIsAtomicUpsertDeleteStale(t *testing.T) {
	t.Parallel()

	query := rebuildCollectorEvidenceSummarySQL
	for _, want := range []string{
		"active_scopes AS (",
		"collector_kind IN (",
		"'git'",
		"'ci_cd_run'",
		"JOIN LATERAL (",
		"FROM fact_records AS fact",
		"AND fact.generation_id = scope.generation_id",
		"AND fact.is_tombstone = FALSE",
		"WHEN fact.fact_kind LIKE 'reducer_%' THEN 'reducer_facts'",
		"COALESCE(NULLIF(BTRIM(fact.source_system), ''), '') AS source_system",
		"COUNT(*) AS observation_count",
		"MAX(fact.observed_at) AS last_observed_at",
		"MAX(fact.ingested_at) AS last_ingested_at",
		"GROUP BY evidence_source, source_system",
		"INSERT INTO collector_evidence_summary",
		"ON CONFLICT (scope_id, generation_id, evidence_source, source_system) DO UPDATE SET",
		"materialized_at = EXCLUDED.materialized_at",
		"DELETE FROM collector_evidence_summary",
		"WHERE NOT EXISTS (",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("resweep SQL missing %q:\n%s", want, query)
		}
	}
	// The resweep stamps the watermark from the bound parameter, never wall clock.
	if !strings.Contains(query, "$1") {
		t.Fatalf("resweep SQL must bind materialized_at as $1:\n%s", query)
	}
	// Private fact payload columns must never reach the summary.
	for _, forbidden := range []string{"fact.payload", "source_uri", "source_record_id"} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("resweep SQL uses private field %q:\n%s", forbidden, query)
		}
	}
}
