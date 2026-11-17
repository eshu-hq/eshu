// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/retirement"
)

// censusCascadeChildren names the 2e census rows the purge reaches through a
// foreign-key ON DELETE CASCADE from a covered parent instead of its own
// statement. The entry is only honored while the parent row is itself covered
// by a purge statement, and TestRetirementCensusCascadeChildrenAreForeignKeyed
// proves the foreign key against the migrated schema, so this map cannot
// excuse a table that has no cascade.
var censusCascadeChildren = map[string]string{
	"service_evidence_snapshots": "service_materialization_generations",
}

// purgeStatementTables returns the tables a purge run issues a DELETE FROM
// for, read from the statements the scripted transaction actually received.
func purgeStatementTables(t *testing.T) map[string]struct{} {
	t.Helper()
	database, tx := stagePurgeScriptedDB()
	purger := NewRetirementPurger(database, database)
	if _, err := purger.PurgeRetired(context.Background(), purgeTestRequest()); err != nil {
		t.Fatalf("PurgeRetired: %v", err)
	}
	deletePattern := regexp.MustCompile(`(?is)\bDELETE\s+FROM\s+(?:ONLY\s+)?"?([a-z_][a-z0-9_]*)"?`)
	covered := make(map[string]struct{})
	record := func(query string) {
		for _, match := range deletePattern.FindAllStringSubmatch(query, -1) {
			covered[strings.ToLower(match[1])] = struct{}{}
		}
	}
	for _, call := range tx.execs {
		record(call.query)
	}
	for _, call := range tx.queries {
		record(call.query)
	}
	return covered
}

// TestRetirementCensus2eRowsHaveACoveringPurgeStatement is the #10820 guard:
// a census row that lists the 2e step must be deleted by a statement the
// purge issues, or be a foreign-key cascade child of a covered parent. A
// census claim with no purge statement behind it is the drift this guard
// stops: the rows survive retirement while the census says they do not.
func TestRetirementCensus2eRowsHaveACoveringPurgeStatement(t *testing.T) {
	t.Parallel()

	covered := purgeStatementTables(t)
	var uncovered []string
	for table, entry := range retirement.RetireTableDispositions {
		if censusRowIsUncovered(entry, covered) {
			uncovered = append(uncovered, table)
		}
	}
	sort.Strings(uncovered)
	if len(uncovered) > 0 {
		t.Fatalf("census rows list 2e but no purge statement deletes them (add a statement in the leftover sweep, "+
			"or correct the census row with the reason it is not purged): %v", uncovered)
	}
}

// TestRetirementCensusGuardSeesAnUncoveredRow is the red/green pair for the
// guard above: a 2e row with no covering statement must be reported. A guard
// that no mutation can turn red proves nothing.
func TestRetirementCensusGuardSeesAnUncoveredRow(t *testing.T) {
	t.Parallel()

	covered := purgeStatementTables(t)
	const probe = "census_guard_probe_table"
	if _, ok := covered[probe]; ok {
		t.Fatalf("probe table %q unexpectedly covered", probe)
	}
	entry := retirement.RetireTableEntry{
		Table: probe, KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2e"},
		Disposition: "2e", Reason: "seeded violation",
	}
	if !censusRowIsUncovered(entry, covered) {
		t.Fatal("guard did not report a 2e row with no purge statement")
	}
	for table := range covered {
		entry.Table = table
		if censusRowIsUncovered(entry, covered) {
			t.Fatalf("guard reported covered table %q as uncovered", table)
		}
		break
	}
}

func censusListsStep(entry retirement.RetireTableEntry, step string) bool {
	for _, s := range entry.RemoveSteps {
		if s == step {
			return true
		}
	}
	return false
}

func censusRowIsUncovered(entry retirement.RetireTableEntry, covered map[string]struct{}) bool {
	if !censusListsStep(entry, "2e") {
		return false
	}
	if _, ok := covered[entry.Table]; ok {
		return false
	}
	if parent, ok := censusCascadeChildren[entry.Table]; ok {
		if _, parentCovered := covered[parent]; parentCovered {
			return false
		}
	}
	return true
}
