// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factwrite

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// scopeLeg is the reducer owner leg (#11213): the conflict clause updates a row
// only while it belongs to the incoming row's scope. There is deliberately no
// generation leg: a reducer id that is stable across generations of one scope
// must keep updating its row, and every reducer id function hashes the scope.
const scopeLeg = "fact_records.scope_id = EXCLUDED.scope_id"

func normalized(sqlText string) string { return strings.Join(strings.Fields(sqlText), " ") }

// TestEveryReducerFactStatementCarriesTheScopeOwnerLeg pins the three conflict
// clauses: the leg is present, comes after the fencing predicate where there is
// one, and no generation leg exists.
func TestEveryReducerFactStatementCarriesTheScopeOwnerLeg(t *testing.T) {
	t.Parallel()

	for name, statement := range map[string]string{
		"SingleInsertQuery":         SingleInsertQuery,
		"BatchInsertQuery":          BatchInsertQuery,
		"BatchInsertVersionedQuery": BatchInsertVersionedQuery,
	} {
		text := normalized(statement)
		leg := strings.Index(text, scopeLeg)
		if leg < 0 {
			t.Errorf("%s has no scope owner leg: a write could change another scope's row", name)
			continue
		}
		if conflict := strings.Index(text, "ON CONFLICT"); leg < conflict {
			t.Errorf("%s: the owner leg sits before ON CONFLICT", name)
		}
		if fence := strings.Index(text, "fact_records.fencing_token <= EXCLUDED.fencing_token"); fence >= 0 && leg < fence {
			t.Errorf("%s: the owner leg precedes the fencing predicate, which stays first and verbatim", name)
		}
		if strings.Contains(text, "fact_records.generation_id = EXCLUDED.generation_id") {
			t.Errorf("%s carries a generation leg: an id stable across generations would freeze at its first generation", name)
		}
	}
}

// TestOwnerLegDriftSeededViolations is the RED/GREEN pair for the pin above: the
// shipped statements are clean, and each planted corruption is reported.
func TestOwnerLegDriftSeededViolations(t *testing.T) {
	t.Parallel()

	if drift := ownerLegDrift(SingleInsertQuery); drift != "" {
		t.Fatalf("GREEN: the shipped single insert reported drift: %s", drift)
	}
	for name, mutated := range map[string]string{
		"leg deleted":     strings.Replace(SingleInsertQuery, scopeLeg, "TRUE", 1),
		"generation leg":  strings.Replace(SingleInsertQuery, scopeLeg, scopeLeg+" AND fact_records.generation_id = EXCLUDED.generation_id", 1),
		"no conflict leg": strings.Replace(SingleInsertQuery, "WHERE "+scopeLeg, "", 1),
	} {
		if ownerLegDrift(mutated) == "" {
			t.Errorf("RED: the planted violation %q was not reported", name)
		}
	}
}

// ownerLegDrift mirrors the pin for one statement and returns the first problem.
func ownerLegDrift(statement string) string {
	text := normalized(statement)
	if !strings.Contains(text, "WHERE "+scopeLeg) && !strings.Contains(text, "AND "+scopeLeg) {
		return "scope owner leg missing"
	}
	if strings.Contains(text, "fact_records.generation_id = EXCLUDED.generation_id") {
		return "generation leg present"
	}
	return ""
}

type countResult struct{ affected int64 }

func (countResult) LastInsertId() (int64, error)   { return 0, nil }
func (r countResult) RowsAffected() (int64, error) { return r.affected, nil }

// TestCheckOwnedReportsASkippedSingleRow: a single-row insert whose conflict WHERE
// skipped the row (zero rows affected) is an owner conflict, never a silent skip.
func TestCheckOwnedReportsASkippedSingleRow(t *testing.T) {
	t.Parallel()

	if err := CheckOwned(context.Background(), nil, countResult{1}, "reducer_single", "fact-1", "scope-b", "gen-1", "workload_identity"); err != nil {
		t.Fatalf("a landed row reported %v", err)
	}
	err := CheckOwned(context.Background(), nil, countResult{0}, "reducer_single", "fact-1", "scope-b", "gen-1", "workload_identity")
	var conflict *OwnerConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("a skipped row reported %v, want *OwnerConflictError", err)
	}
	if conflict.Writer != "reducer_single" || conflict.Count != 1 || len(conflict.Samples) != 1 ||
		conflict.Samples[0].FactID != "fact-1" || conflict.Samples[0].IncomingScopeID != "scope-b" {
		t.Fatalf("conflict = %+v", conflict)
	}
	if conflict.Retryable() {
		t.Error("an owner conflict is deterministic: retrying it only repeats it")
	}
	if conflict.FailureClass() != OwnerConflictFailureClass || OwnerConflictFailureClass != "fact_id_owner_conflict" {
		t.Errorf("failure class = %q", conflict.FailureClass())
	}
}

// TestShortfallProbeClassifiesForeignRows covers the batch path: a shortfall
// with a handle that cannot probe stays silent (the fakes of existing writers),
// and a shortfall the probe resolves to foreign-held ids is an owner conflict
// while a shortfall with no foreign row (a fenced-out stale write) is not.
func TestShortfallProbeClassifiesForeignRows(t *testing.T) {
	t.Parallel()

	rows := []ownerRow{
		{FactID: "f1", ScopeID: "scope-b", GenerationID: "g", FactKind: "k"},
		{FactID: "f2", ScopeID: "scope-b", GenerationID: "g", FactKind: "k"},
		{FactID: "f3", ScopeID: "scope-b", GenerationID: "g", FactKind: "k"},
	}
	// No shortfall: nothing to resolve.
	if err := resolveShortfall(context.Background(), countResult{3}, "reducer_batch", rows, nil); err != nil {
		t.Fatalf("a full batch reported %v", err)
	}
	// A shortfall with no prober is not resolvable here and stays silent.
	if err := resolveShortfall(context.Background(), countResult{1}, "reducer_batch", rows, nil); err != nil {
		t.Fatalf("a shortfall with no prober reported %v", err)
	}
	probe := func(context.Context, []ownerRow) ([]ownerHeld, error) {
		return []ownerHeld{{FactID: "f2", StoredScopeID: "scope-a"}}, nil
	}
	err := resolveShortfall(context.Background(), countResult{2}, "reducer_batch", rows, probe)
	var conflict *OwnerConflictError
	if !errors.As(err, &conflict) || conflict.Count != 1 || conflict.Samples[0].FactID != "f2" ||
		conflict.Samples[0].StoredScopeID != "scope-a" || conflict.Writer != "reducer_batch" {
		t.Fatalf("a foreign-held row reported %v, want one conflict on f2 held by scope-a", err)
	}
	none := func(context.Context, []ownerRow) ([]ownerHeld, error) { return nil, nil }
	if err := resolveShortfall(context.Background(), countResult{2}, "reducer_batch", rows, none); err != nil {
		t.Fatalf("a fenced-out shortfall with no foreign row reported %v", err)
	}
	failing := func(context.Context, []ownerRow) ([]ownerHeld, error) { return nil, errors.New("boom") }
	if err := resolveShortfall(context.Background(), countResult{2}, "reducer_batch", rows, failing); err == nil {
		t.Fatal("a failing probe must surface, not read as a clean batch")
	}
}
