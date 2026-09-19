// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
	"time"
)

var seedRowsTestNow = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestScopeRowsShapesEveryColumn(t *testing.T) {
	scopes := []SeedScope{
		{ScopeID: "scope-1", CollectorKind: "git", ActiveGenerationID: "scope-1-gen-0"},
	}
	rows := scopeRows(scopes, seedRowsTestNow)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	row := rows[0]
	if len(row) != len(scopeColumns) {
		t.Fatalf("len(row) = %d, want %d (len(scopeColumns))", len(row), len(scopeColumns))
	}
	if row[0] != "scope-1" {
		t.Errorf("scope_id = %v, want scope-1", row[0])
	}
	if row[4] != "git" {
		t.Errorf("collector_kind = %v, want git", row[4])
	}
	if row[9] != "scope-1-gen-0" {
		t.Errorf("active_generation_id = %v, want scope-1-gen-0", row[9])
	}
}

func TestGenerationRowsMarksExactlyOneActivePerScope(t *testing.T) {
	generations := []SeedGeneration{
		{GenerationID: "g0", ScopeID: "scope-1", Active: false},
		{GenerationID: "g1", ScopeID: "scope-1", Active: false},
		{GenerationID: "g2", ScopeID: "scope-1", Active: true},
	}
	rows := generationRows(generations, seedRowsTestNow)
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}

	statusIdx := indexOf(generationColumns, "status")
	activatedAtIdx := indexOf(generationColumns, "activated_at")

	activeCount := 0
	for i, row := range rows {
		status := row[statusIdx].(string)
		if status == "active" {
			activeCount++
			if row[activatedAtIdx] == nil {
				t.Errorf("row %d: active generation has nil activated_at", i)
			}
		} else if status != "superseded" {
			t.Errorf("row %d: status = %q, want active or superseded", i, status)
		}
	}
	if activeCount != 1 {
		t.Errorf("active generation count = %d, want 1 (matches scope_generations_active_scope_idx unique constraint)", activeCount)
	}
}

func TestWorkItemRowsPreservesStatus(t *testing.T) {
	items := []SeedWorkItem{
		{WorkItemID: "wi-1", ScopeID: "scope-1", GenerationID: "g0", Stage: "reducer", Domain: "repository", Status: "pending"},
	}
	rows := workItemRows(items, seedRowsTestNow)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	statusIdx := indexOf(workItemColumns, "status")
	if rows[0][statusIdx] != "pending" {
		t.Errorf("status = %v, want pending", rows[0][statusIdx])
	}
}

func indexOf(columns []string, name string) int {
	for i, c := range columns {
		if c == name {
			return i
		}
	}
	panic("column not found: " + name)
}
