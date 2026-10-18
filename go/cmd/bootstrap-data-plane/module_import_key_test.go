// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// TestRunLeavesTheMarkerUnsetWhileModulesStayUnkeyed proves the schema marker
// is the last step: a verify that still finds an unkeyed canonical Module
// fails the run before the marker, so no writer on the new fingerprint is
// admitted into a graph its import_key MERGE would duplicate.
func TestRunLeavesTheMarkerUnsetWhileModulesStayUnkeyed(t *testing.T) {
	t.Parallel()

	var events []string
	var logs bytes.Buffer
	db, err := runWithGraphRunner(t, "neo4j", &fakeGraphRunner{events: &events, moduleUnkeyed: 2}, &events, &logs)
	if err == nil || !strings.Contains(err.Error(), "2 unkeyed canonical Module nodes") {
		t.Fatalf("run() error = %v, want the unkeyed refusal", err)
	}
	if markerWritten(db) {
		t.Fatal("schema marker written while canonical Module nodes are unkeyed")
	}
	if !strings.Contains(logs.String(), "bootstrap.graph.module_import_key.incomplete") {
		t.Errorf("logs missing the incomplete event: %s", logs.String())
	}
}

// TestRunStopsBeforeTheMarkerWhenTheCollapseFails proves a failed collapse
// statement stops the run with its stage named, and nothing after it runs.
func TestRunStopsBeforeTheMarkerWhenTheCollapseFails(t *testing.T) {
	t.Parallel()

	var events []string
	var logs bytes.Buffer
	runner := &fakeGraphRunner{events: &events, moduleErr: errors.New("connection reset")}
	db, err := runWithGraphRunner(t, "neo4j", runner, &events, &logs)
	if err == nil || !strings.Contains(err.Error(), "module import key collapse: connection reset") {
		t.Fatalf("run() error = %v, want the collapse stage error", err)
	}
	if markerWritten(db) {
		t.Fatal("schema marker written after a failed collapse")
	}
	if last := events[len(events)-1]; last != "module_collapse" {
		t.Fatalf("last event = %q, want module_collapse; events = %v", last, events)
	}
}

// TestRunLogsTheModuleMigrationCounts proves a clean migration logs its
// operator event with the counts.
func TestRunLogsTheModuleMigrationCounts(t *testing.T) {
	t.Parallel()

	var events []string
	var logs bytes.Buffer
	db, err := runWithGraphRunner(t, "neo4j", &fakeGraphRunner{events: &events}, &events, &logs)
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !markerWritten(db) {
		t.Fatal("schema marker not written after a clean migration")
	}
	for _, want := range []string{"bootstrap.graph.module_import_key.migrated", `"unkeyed_remaining":0`, `"repointed_imports":0`} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("logs missing %s", want)
		}
	}
}

// TestModuleImportKeyMigrationCypherShape pins what makes the migration safe
// to rerun and safe under the constraint: it selects only unkeyed canonical
// nodes, keys and collapses per group in batched transactions, and moves
// edges without overwriting an edge the keeper already has.
func TestModuleImportKeyMigrationCypherShape(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"WHERE m.uid IS NULL AND m.import_key IS NULL AND m.name IS NOT NULL",
		"WITH n ORDER BY n.import_key IS NULL, elementId(n)",
		"SET keeper.import_key = name + '@' + lang",
		"ON CREATE SET k = properties(r)",
		"DETACH DELETE extra",
		"} IN TRANSACTIONS OF $batch_size ROWS",
	} {
		if !strings.Contains(moduleImportKeyCollapseCypher, want) {
			t.Errorf("collapse Cypher missing %q", want)
		}
	}
	for _, want := range []string{
		"WHERE s.uid IS NOT NULL",
		"MERGE (m:Module {import_key: s.name + '@' + coalesce(s.lang, '')})",
		"DELETE r",
		"} IN TRANSACTIONS OF $batch_size ROWS",
	} {
		if !strings.Contains(moduleImportKeyRepointCypher, want) {
			t.Errorf("re-point Cypher missing %q", want)
		}
	}
	if strings.Contains(moduleImportKeyRepointCypher, "DETACH DELETE") {
		t.Error("re-point Cypher deletes a node; it must only move IMPORTS edges off the semantic Module")
	}
}

// TestAdoptionCannotMarkBeforeTheModuleMigration proves the adoption path,
// which skips the graph DDL, still cannot write the schema marker until the
// Module import_key migration has run and verified: a complete catalog with an
// unkeyed canonical Module, or a failed collapse, leaves the marker unset and
// applies no DDL.
func TestAdoptionCannotMarkBeforeTheModuleMigration(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		runner  func(*[]string) *fakeGraphRunner
		wantErr string
	}{
		{
			name:    "unkeyed canonical module",
			runner:  func(e *[]string) *fakeGraphRunner { return &fakeGraphRunner{events: e, moduleUnkeyed: 1} },
			wantErr: "1 unkeyed canonical Module nodes",
		},
		{
			name: "collapse fails",
			runner: func(e *[]string) *fakeGraphRunner {
				return &fakeGraphRunner{events: e, moduleErr: errors.New("leader switch")}
			},
			wantErr: "module import key collapse: leader switch",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var events []string
			db, ddl, err := runNeo4jAdoption(t, tc.runner(&events), &events)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("run() error = %v, want %q", err, tc.wantErr)
			}
			if ddl {
				t.Fatal("adoption applied DDL")
			}
			if markerWritten(db) {
				t.Fatalf("adoption wrote the schema marker before the Module migration verified; events = %v", events)
			}
			if !strings.Contains(strings.Join(events, ","), "module_collapse") {
				t.Fatalf("events = %v, want the Module migration to have run on the adoption path", events)
			}
		})
	}
}
