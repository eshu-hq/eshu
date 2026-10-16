// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factkeys

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

type recordedExec struct {
	query string
	args  []any
}

type recordingExecutor struct {
	calls  []recordedExec
	rows   int64
	failOn string
}

func (e *recordingExecutor) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	e.calls = append(e.calls, recordedExec{query: query, args: args})
	if e.failOn != "" && strings.Contains(query, e.failOn) {
		return nil, errors.New("boom")
	}
	return rowsResult(e.rows), nil
}

type rowsResult int64

func (r rowsResult) LastInsertId() (int64, error) { return 0, nil }
func (r rowsResult) RowsAffected() (int64, error) { return int64(r), nil }

func tableOf(query string) string {
	for _, table := range []string{RelationshipReferenceTable, ManifestConsumptionTable, RegistryIdentityTable} {
		if strings.Contains(query, "DELETE FROM "+table+"\n") {
			return table
		}
	}
	return ""
}

// TestDeleteGenerationsRunsTableMajorInLockOrder pins the statement order
// (every generation of one table before the next table, tables in lock
// order), the bound arguments, and the per-table row sums.
func TestDeleteGenerationsRunsTableMajorInLockOrder(t *testing.T) {
	t.Parallel()
	exec := &recordingExecutor{rows: 3}
	rowsPruned := map[string]int64{}
	if err := DeleteGenerations(context.Background(), exec, []string{"s1", "s2"}, []string{"g1", "g2"}, rowsPruned); err != nil {
		t.Fatalf("DeleteGenerations() error = %v", err)
	}
	var got []string
	for _, call := range exec.calls {
		if !strings.HasPrefix(call.query, DeleteMarker) || !strings.Contains(call.query, "WHERE scope_id = $1 AND generation_id = $2\n") {
			t.Fatalf("statement %q is not a marked (scope_id, generation_id) delete", call.query)
		}
		got = append(got, fmt.Sprintf("%s %v/%v", tableOf(call.query), call.args[0], call.args[1]))
	}
	want := []string{
		RelationshipReferenceTable + " s1/g1", RelationshipReferenceTable + " s2/g2",
		ManifestConsumptionTable + " s1/g1", ManifestConsumptionTable + " s2/g2",
		RegistryIdentityTable + " s1/g1", RegistryIdentityTable + " s2/g2",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("statements = %v, want %v", got, want)
	}
	for _, table := range []string{RelationshipReferenceTable, ManifestConsumptionTable, RegistryIdentityTable} {
		if rowsPruned[table] != 6 {
			t.Errorf("rowsPruned[%s] = %d, want 6", table, rowsPruned[table])
		}
	}
}

// TestDeletePageBindsPageUnderScopeAndGeneration pins the page form.
func TestDeletePageBindsPageUnderScopeAndGeneration(t *testing.T) {
	t.Parallel()
	exec := &recordingExecutor{rows: 2}
	page := []string{"f1", "f2"}
	deleted, err := DeletePage(context.Background(), exec, "s1", "g1", page)
	if err != nil {
		t.Fatalf("DeletePage() error = %v", err)
	}
	if len(exec.calls) != 3 {
		t.Fatalf("statements = %d, want 3", len(exec.calls))
	}
	for i, table := range []string{RelationshipReferenceTable, ManifestConsumptionTable, RegistryIdentityTable} {
		call := exec.calls[i]
		ids, _ := call.args[2].([]string)
		if tableOf(call.query) != table || !strings.Contains(call.query, "AND fact_id = ANY($3::text[])") ||
			call.args[0] != "s1" || call.args[1] != "g1" || !slices.Equal(ids, page) {
			t.Fatalf("statement %d = %q %v, want %s page delete", i, call.query, call.args, table)
		}
		if deleted[table] != 2 {
			t.Errorf("deleted[%s] = %d, want 2", table, deleted[table])
		}
	}
}

// TestDeleteStopsAtFirstError proves a failed statement surfaces with its
// table and stops before the later tables.
func TestDeleteStopsAtFirstError(t *testing.T) {
	t.Parallel()
	exec := &recordingExecutor{failOn: ManifestConsumptionTable}
	err := DeleteGenerations(context.Background(), exec, []string{"s1"}, []string{"g1"}, map[string]int64{})
	if err == nil || !strings.Contains(err.Error(), ManifestConsumptionTable) {
		t.Fatalf("DeleteGenerations() error = %v, want a %s failure", err, ManifestConsumptionTable)
	}
	if len(exec.calls) != 2 {
		t.Fatalf("statements after failure = %d, want 2 (registry keys never run)", len(exec.calls))
	}
	if err := DeleteGenerations(context.Background(), exec, []string{"s1"}, nil, map[string]int64{}); err == nil {
		t.Fatal("DeleteGenerations() with mismatched ids error = nil")
	}
}
