// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// stagedCompactRow builds one narrow compact input row.
func stagedCompactRow(factID, key, findingID string, priority int, hasPayload bool, suppression string) StagedCandidateRow {
	return StagedCandidateRow{
		FactID:              factID,
		CanonicalKey:        key,
		FindingID:           findingID,
		PriorityScore:       priority,
		HasPayloadFindingID: hasPayload,
		SuppressionState:    suppression,
	}
}

// TestCompactStagedAddBatchRoutesByScope proves the tick loop never names
// the operator scope: operator rows land in the override map, all other
// scopes in the source winners.
func TestCompactStagedAddBatchRoutesByScope(t *testing.T) {
	compactor := NewStagedCompactor(20000, 1000000, 250000)
	if err := compactor.AddBatch("image://repo/scope", []StagedCandidateRow{
		stagedCompactRow("f1", "k1", "", 0, false, "active"),
	}); err != nil {
		t.Fatalf("AddBatch source: %v", err)
	}
	if err := compactor.AddBatch("operator:vulnerability_suppressions", []StagedCandidateRow{
		stagedCompactRow("f2", "k1", "", 0, false, "suppressed"),
	}); err != nil {
		t.Fatalf("AddBatch operator: %v", err)
	}
	if _, ok := compactor.SourceWinners()["k1"]; !ok {
		t.Fatal("source scope row missing from source winners")
	}
	override, ok := compactor.Overrides()["k1"]
	if !ok {
		t.Fatal("operator scope row missing from overrides")
	}
	if override.SuppressionState != "suppressed" {
		t.Fatalf("override suppression = %q, want suppressed", override.SuppressionState)
	}
}

// stagedCompactStored builds a stored winner carrying the compared narrow
// fields; wide payload columns default empty and are pinned by winner fact.
func stagedCompactStored(key, winnerFactID, findingID string, priority, sources int, suppression string) StoredWinner {
	return StoredWinner{
		CanonicalKey:     key,
		WinnerFactID:     winnerFactID,
		FindingID:        findingID,
		PriorityScore:    priority,
		SourceCount:      sources,
		SuppressionState: suppression,
	}
}

func TestCompactStagedElectsPriorityThenPayloadThenFactID(t *testing.T) {
	t.Parallel()
	c := NewStagedCompactor(20000, 1000000, 250000)
	rows := []StagedCandidateRow{
		stagedCompactRow("fact-b", "k1", "k1", 5, true, "active"),
		stagedCompactRow("fact-a", "k1", "k1", 5, true, "active"),
		stagedCompactRow("fact-c", "k1", "F-real", 5, true, "active"),
		stagedCompactRow("fact-d", "k1", "k1", 9, false, "active"),
		stagedCompactRow("fact-e", "k1", "k1", 3, true, "active"),
	}
	// Lowest priority first across two batches: the priority-9 row must win
	// even though it arrives in the later batch.
	if err := c.AddSourceBatch(rows[:2]); err != nil {
		t.Fatalf("AddSourceBatch: %v", err)
	}
	if err := c.AddSourceBatch(rows[2:]); err != nil {
		t.Fatalf("AddSourceBatch: %v", err)
	}
	got, ok := c.SourceWinners()["k1"]
	if !ok {
		t.Fatal("missing winner for k1")
	}
	if got.WinnerFactID != "fact-d" {
		t.Fatalf("winner = %q, want priority-9 fact-d", got.WinnerFactID)
	}
	if got.SourceCount != 5 {
		t.Fatalf("sourceCount = %d, want 5", got.SourceCount)
	}
}

func TestCompactStagedPayloadFlagBreaksPriorityTie(t *testing.T) {
	t.Parallel()
	c := NewStagedCompactor(20000, 1000000, 250000)
	if err := c.AddSourceBatch([]StagedCandidateRow{
		stagedCompactRow("fact-a", "k1", "k1", 5, false, "active"),
		stagedCompactRow("fact-b", "k1", "F-real", 5, true, "active"),
	}); err != nil {
		t.Fatalf("AddSourceBatch: %v", err)
	}
	if got := c.SourceWinners()["k1"].WinnerFactID; got != "fact-b" {
		t.Fatalf("winner = %q, want fact-b with payload finding id", got)
	}
}

func TestCompactStagedFactIDBreaksFullTie(t *testing.T) {
	t.Parallel()
	c := NewStagedCompactor(20000, 1000000, 250000)
	if err := c.AddSourceBatch([]StagedCandidateRow{
		stagedCompactRow("fact-b", "k1", "F", 5, true, "active"),
		stagedCompactRow("fact-a", "k1", "F", 5, true, "active"),
	}); err != nil {
		t.Fatalf("AddSourceBatch: %v", err)
	}
	if got := c.SourceWinners()["k1"].WinnerFactID; got != "fact-a" {
		t.Fatalf("winner = %q, want lowest fact_id fact-a", got)
	}
}

func TestCompactStagedSourceCountAccumulatesAcrossBatches(t *testing.T) {
	t.Parallel()
	c := NewStagedCompactor(20000, 1000000, 250000)
	for i := 0; i < 4; i++ {
		rows := []StagedCandidateRow{
			stagedCompactRow(fmt.Sprintf("fact-%d-a", i), "k1", "F", 1, true, "active"),
			stagedCompactRow(fmt.Sprintf("fact-%d-b", i), "k2", "G", 2, true, "active"),
		}
		if err := c.AddSourceBatch(rows); err != nil {
			t.Fatalf("AddSourceBatch %d: %v", i, err)
		}
	}
	if got := c.SourceWinners()["k1"].SourceCount; got != 4 {
		t.Fatalf("k1 sourceCount = %d, want 4", got)
	}
	if got := c.SourceWinners()["k2"].SourceCount; got != 4 {
		t.Fatalf("k2 sourceCount = %d, want 4", got)
	}
	if got := c.StagedFacts(); got != 8 {
		t.Fatalf("staged facts = %d, want 8", got)
	}
}

func TestCompactStagedCrossScopeDuplicatesResolveLikeSingleBatch(t *testing.T) {
	t.Parallel()
	dupes := []StagedCandidateRow{
		stagedCompactRow("fact-b", "k1", "F", 5, true, "active"),
		stagedCompactRow("fact-a", "k1", "F", 5, true, "active"),
		stagedCompactRow("fact-c", "k1", "F", 7, false, "active"),
	}
	single := NewStagedCompactor(20000, 1000000, 250000)
	if err := single.AddSourceBatch(dupes); err != nil {
		t.Fatalf("single AddSourceBatch: %v", err)
	}
	split := NewStagedCompactor(20000, 1000000, 250000)
	// One row per batch emulates the same key recurring across scopes.
	for _, row := range dupes {
		if err := split.AddSourceBatch([]StagedCandidateRow{row}); err != nil {
			t.Fatalf("split AddSourceBatch: %v", err)
		}
	}
	want := single.SourceWinners()["k1"]
	got := split.SourceWinners()["k1"]
	if want != got {
		t.Fatalf("split winner %+v != single-batch winner %+v", got, want)
	}
}

func TestCompactStagedOverridesStaySeparateUntilDiff(t *testing.T) {
	t.Parallel()
	c := NewStagedCompactor(20000, 1000000, 250000)
	if err := c.AddSourceBatch([]StagedCandidateRow{
		stagedCompactRow("fact-s", "k1", "F", 5, true, "active"),
	}); err != nil {
		t.Fatalf("AddSourceBatch: %v", err)
	}
	if err := c.AddOverrideBatch([]StagedCandidateRow{
		stagedCompactRow("fact-o", "k1", "", 3, false, "suppressed"),
	}); err != nil {
		t.Fatalf("AddOverrideBatch: %v", err)
	}
	winner := c.SourceWinners()["k1"]
	if winner.WinnerFactID != "fact-s" || winner.SuppressionState != "active" {
		t.Fatalf("source winner polluted by override: %+v", winner)
	}
	override, ok := c.Overrides()["k1"]
	if !ok || override.FactID != "fact-o" || override.SuppressionState != "suppressed" {
		t.Fatalf("override missing or wrong: %+v ok=%v", override, ok)
	}
}

func TestDiffCandidatesOverrideKeyAlwaysEmitted(t *testing.T) {
	t.Parallel()
	source := map[string]StagedWinner{
		"k1": {CanonicalKey: "k1", WinnerFactID: "fact-s", FindingID: "F", PriorityScore: 5, HasPayloadFindingID: true, SuppressionState: "active", SourceCount: 1},
	}
	overrides := map[string]StagedOverride{
		"k1": {CanonicalKey: "k1", FactID: "fact-o", SuppressionState: "suppressed"},
	}
	// Stored matches the source winner exactly: without the override there
	// would be nothing to emit.
	stored := map[string]StoredWinner{
		"k1": stagedCompactStored("k1", "fact-s", "F", 5, 1, "active"),
	}
	got := DiffCandidates(source, overrides, stored)
	if len(got) != 1 || got[0] != "k1" {
		t.Fatalf("DiffCandidates = %v, want [k1] (override forces emission)", got)
	}
}

func TestDiffCandidatesEmitsNewChangedAndDeletedKeys(t *testing.T) {
	t.Parallel()
	source := map[string]StagedWinner{
		"same":    {CanonicalKey: "same", WinnerFactID: "fact-a", FindingID: "F", PriorityScore: 1, SuppressionState: "active", SourceCount: 2},
		"new":     {CanonicalKey: "new", WinnerFactID: "fact-n", FindingID: "N", PriorityScore: 1, SuppressionState: "active", SourceCount: 1},
		"changed": {CanonicalKey: "changed", WinnerFactID: "fact-b2", FindingID: "F", PriorityScore: 1, SuppressionState: "active", SourceCount: 1},
		"counted": {CanonicalKey: "counted", WinnerFactID: "fact-c", FindingID: "F", PriorityScore: 1, SuppressionState: "active", SourceCount: 3},
	}
	stored := map[string]StoredWinner{
		"same":    stagedCompactStored("same", "fact-a", "F", 1, 2, "active"),
		"changed": stagedCompactStored("changed", "fact-b1", "F", 1, 1, "active"),
		"counted": stagedCompactStored("counted", "fact-c", "F", 1, 2, "active"),
		"gone":    stagedCompactStored("gone", "fact-g", "G", 1, 1, "active"),
	}
	got := DiffCandidates(source, nil, stored)
	sort.Strings(got)
	want := []string{"changed", "counted", "gone", "new"}
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		t.Fatalf("DiffCandidates = %v, want %v", got, want)
	}
}

func TestDiffCandidatesEmptyInputsEmitNothing(t *testing.T) {
	t.Parallel()
	if got := DiffCandidates(nil, nil, nil); len(got) != 0 {
		t.Fatalf("DiffCandidates on nil maps = %v, want empty", got)
	}
	if got := DiffCandidates(map[string]StagedWinner{}, map[string]StagedOverride{}, map[string]StoredWinner{}); len(got) != 0 {
		t.Fatalf("DiffCandidates on empty maps = %v, want empty", got)
	}
}

func TestDiffCandidatesEmptyStoredEmitsEverySourceKey(t *testing.T) {
	t.Parallel()
	source := map[string]StagedWinner{
		"k1": {CanonicalKey: "k1", WinnerFactID: "fact-a", SourceCount: 1},
		"k2": {CanonicalKey: "k2", WinnerFactID: "fact-b", SourceCount: 1},
	}
	got := DiffCandidates(source, nil, map[string]StoredWinner{})
	sort.Strings(got)
	if fmt.Sprintf("%v", got) != "[k1 k2]" {
		t.Fatalf("DiffCandidates = %v, want [k1 k2]", got)
	}
}

func TestCompactStagedOverBudgetBatchAborts(t *testing.T) {
	t.Parallel()
	c := NewStagedCompactor(2, 1000000, 250000)
	err := c.AddSourceBatch([]StagedCandidateRow{
		stagedCompactRow("fact-a", "k1", "F", 1, true, "active"),
		stagedCompactRow("fact-b", "k2", "F", 1, true, "active"),
		stagedCompactRow("fact-c", "k3", "F", 1, true, "active"),
	})
	over, ok := asStagedOverBudget(err)
	if !ok {
		t.Fatalf("err = %v, want typed over-budget error", err)
	}
	if over.Limit != 2 || over.Got != 3 {
		t.Fatalf("over-budget = %+v, want got=3 limit=2", over)
	}
	if got := c.StagedFacts(); got != 0 {
		t.Fatalf("staged facts after aborted batch = %d, want 0 (never truncate)", got)
	}
}

func TestCompactStagedOverBudgetTotalAborts(t *testing.T) {
	t.Parallel()
	c := NewStagedCompactor(20000, 3, 250000)
	if err := c.AddSourceBatch([]StagedCandidateRow{
		stagedCompactRow("fact-a", "k1", "F", 1, true, "active"),
		stagedCompactRow("fact-b", "k2", "F", 1, true, "active"),
	}); err != nil {
		t.Fatalf("first AddSourceBatch: %v", err)
	}
	err := c.AddSourceBatch([]StagedCandidateRow{
		stagedCompactRow("fact-c", "k3", "F", 1, true, "active"),
		stagedCompactRow("fact-d", "k4", "F", 1, true, "active"),
	})
	if _, ok := asStagedOverBudget(err); !ok {
		t.Fatalf("err = %v, want typed over-budget error", err)
	}
	if got := c.StagedFacts(); got != 2 {
		t.Fatalf("staged facts after aborted batch = %d, want 2 (second batch dropped whole)", got)
	}
}

func TestCompactStagedOverBudgetDistinctKeysAborts(t *testing.T) {
	t.Parallel()
	c := NewStagedCompactor(20000, 1000000, 2)
	if err := c.AddSourceBatch([]StagedCandidateRow{
		stagedCompactRow("fact-a", "k1", "F", 1, true, "active"),
		stagedCompactRow("fact-b", "k2", "F", 1, true, "active"),
	}); err != nil {
		t.Fatalf("first AddSourceBatch: %v", err)
	}
	err := c.AddSourceBatch([]StagedCandidateRow{
		stagedCompactRow("fact-c", "k3", "F", 1, true, "active"),
	})
	if _, ok := asStagedOverBudget(err); !ok {
		t.Fatalf("err = %v, want typed over-budget error", err)
	}
	if _, ok := c.SourceWinners()["k3"]; ok {
		t.Fatal("k3 winner present after aborted batch (must never partially apply)")
	}
}

func TestCompactStagedEmptyBatchIsNoOp(t *testing.T) {
	t.Parallel()
	c := NewStagedCompactor(20000, 1000000, 250000)
	if err := c.AddSourceBatch(nil); err != nil {
		t.Fatalf("nil AddSourceBatch: %v", err)
	}
	if err := c.AddOverrideBatch(nil); err != nil {
		t.Fatalf("nil AddOverrideBatch: %v", err)
	}
	if got := c.StagedFacts(); got != 0 {
		t.Fatalf("staged facts = %d, want 0", got)
	}
	if got := DiffCandidates(c.SourceWinners(), c.Overrides(), map[string]StoredWinner{}); len(got) != 0 {
		t.Fatalf("diff on empty compactor = %v, want empty", got)
	}
}

// stubStagedRows implements db.Rows over an in-memory table for the
// stored-winners fetch proof.
type stubStagedRows struct {
	cols [][]any
	pos  int
}

func (r *stubStagedRows) Next() bool {
	r.pos++
	return r.pos <= len(r.cols)
}

func (r *stubStagedRows) Scan(dest ...any) error {
	row := r.cols[r.pos-1]
	if len(dest) != len(row) {
		return fmt.Errorf("scan arity %d != %d", len(dest), len(row))
	}
	for i := range row {
		switch d := dest[i].(type) {
		case *string:
			s, _ := row[i].(string)
			*d = s
		case *int:
			*d = row[i].(int)
		case *int32:
			*d = int32(row[i].(int))
		case *[]byte:
			switch v := row[i].(type) {
			case string:
				*d = []byte(v)
			case []byte:
				*d = v
			case nil:
				*d = nil
			default:
				return fmt.Errorf("unsupported scan value %T", row[i])
			}
		case *sql.NullTime:
			switch v := row[i].(type) {
			case nil:
				*d = sql.NullTime{}
			case time.Time:
				*d = sql.NullTime{Time: v, Valid: true}
			default:
				return fmt.Errorf("unsupported scan value %T", row[i])
			}
		default:
			return fmt.Errorf("unsupported scan target %T", dest[i])
		}
	}
	return nil
}

func (r *stubStagedRows) Err() error { return nil }

func (r *stubStagedRows) Close() error { return nil }

// stubStagedQueryer implements db.Queryer with one canned cursor.
type stubStagedQueryer struct{ rows db.Rows }

func (s stubStagedQueryer) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return s.rows, nil
}

func TestFetchStoredWinnersReadsEveryComparedColumn(t *testing.T) {
	t.Parallel()
	// Column order must match stagedStoredWinnersSQL: canonical_key plus
	// every data column of supply_chain_impact_canonical_winners in table
	// order (minus materialized_at), so the fetcher cannot silently drop a
	// column the FULL JOIN ROW(...) comparison uses.
	_ = len(supplyChainImpactWinnerDataColumns)
	rows := &stubStagedRows{cols: [][]any{
		{"k1", "fact-a", "scope-a", "F", 5, 2, "exploitable", "go", "high", "repo", "CVE-1", "ADV-1", "pkg", "digest", "img", "p1", "prof", "1.0", "reason", "active", nil, "[]", "[]", "[]"},
	}}
	stored, err := FetchStoredWinners(context.Background(), stubStagedQueryer{rows: rows})
	if err != nil {
		t.Fatalf("FetchStoredWinners: %v", err)
	}
	got, ok := stored["k1"]
	if !ok {
		t.Fatal("missing stored winner for k1")
	}
	if got.WinnerFactID != "fact-a" || got.WinnerScopeID != "scope-a" || got.FindingID != "F" {
		t.Fatalf("identity columns wrong: %+v", got)
	}
	if got.PriorityScore != 5 || got.SourceCount != 2 {
		t.Fatalf("numeric columns wrong: %+v", got)
	}
	if got.SeverityBucket != "high" || got.CVEID != "CVE-1" || got.ImpactStatus != "exploitable" {
		t.Fatalf("wide payload columns wrong: %+v", got)
	}
	if got.ServiceIDs != "[]" || got.WorkloadIDs != "[]" || got.Environments != "[]" {
		t.Fatalf("jsonb columns wrong: %+v", got)
	}
	if got.SuppressionExpiresAt.Valid {
		t.Fatalf("suppression_expires_at valid: %+v", got.SuppressionExpiresAt)
	}
}

func TestCompactStagedScaleKeepsLiveMapBounded(t *testing.T) {
	t.Parallel()
	const distinct = 50000
	const total = 400000
	c := NewStagedCompactor(20000, 1000000, 250000)
	started := time.Now()
	for base := 0; base < total; base += 20000 {
		batch := make([]StagedCandidateRow, 0, 20000)
		for i := 0; i < 20000 && base+i < total; i++ {
			n := base + i
			key := fmt.Sprintf("key-%05d", n%distinct)
			batch = append(batch, stagedCompactRow(
				fmt.Sprintf("fact-%06d", n), key, "F-"+key, n%11, n%3 == 0, "active",
			))
		}
		if err := c.AddSourceBatch(batch); err != nil {
			t.Fatalf("AddSourceBatch at %d: %v", base, err)
		}
	}
	elapsed := time.Since(started)
	if got := len(c.SourceWinners()); got != distinct {
		t.Fatalf("distinct winners = %d, want %d", got, distinct)
	}
	if got := c.StagedFacts(); got != total {
		t.Fatalf("staged facts = %d, want %d", got, total)
	}
	// The live footprint is the per-key map only: one entry per distinct
	// key however many batches streamed through it.
	t.Logf("compacted %d rows into %d distinct keys in %s", total, len(c.SourceWinners()), elapsed)
	// Spot-check the total order on a key fed across all eight batches.
	winner := c.SourceWinners()["key-00000"]
	if winner.PriorityScore != 10 {
		t.Fatalf("key-00000 winner priority = %d, want 10", winner.PriorityScore)
	}
}
