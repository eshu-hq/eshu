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

// TestModuleImportKeyIncompleteErrorNamesTheWriterPrecondition proves the
// refusal tells the operator what to do, not only that it failed. The usual
// cause of a failed verify is a writer from the previous release still running:
// the Helm schema Job is a pre-upgrade hook, so old pods keep writing while it
// runs, and the fingerprint refuses them only after the marker is written. The
// error and the ERROR log must carry both counts, the instruction to stop those
// writers first, and where the procedure is documented.
func TestModuleImportKeyIncompleteErrorNamesTheWriterPrecondition(t *testing.T) {
	t.Parallel()

	var events []string
	var logs bytes.Buffer
	runner := &fakeGraphRunner{events: &events, moduleUnkeyed: 197, moduleAdopted: 50}
	db, err := runWithGraphRunner(t, "neo4j", runner, &events, &logs)
	if err == nil {
		t.Fatal("run() error = nil, want the incomplete-migration refusal")
	}
	for _, want := range []string{
		"197 unkeyed canonical Module nodes",
		"50 IMPORTS edges on semantic Modules",
		"stop or scale to zero every ingester, projector, resolution engine, and eshu-bootstrap-index from the previous release",
		"then rerun bootstrap",
		moduleImportKeyRunbook,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("run() error = %q, missing %q", err.Error(), want)
		}
	}
	if markerWritten(db) {
		t.Fatal("schema marker written while the migration is incomplete")
	}
	for _, want := range []string{`"unkeyed_remaining":197`, `"adopted_imports_remaining":50`, `"runbook":"` + moduleImportKeyRunbook + `"`} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("incomplete log missing %s: %s", want, logs.String())
		}
	}
}

// TestRunLeavesTheMarkerUnsetOnAnUnreadableVerify proves the verify answer is
// read strictly: a verify that returns no row is a refusal, not two zeros.
func TestRunLeavesTheMarkerUnsetOnAnUnreadableVerify(t *testing.T) {
	t.Parallel()

	var events []string
	var logs bytes.Buffer
	db, err := runWithGraphRunner(t, "neo4j", &fakeGraphRunner{events: &events, moduleVerifyEmpty: true}, &events, &logs)
	if err == nil || !strings.Contains(err.Error(), "module import key verify") {
		t.Fatalf("run() error = %v, want the verify refusal", err)
	}
	if markerWritten(db) {
		t.Fatal("schema marker written on a verify that returned no row")
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
	for _, want := range []string{"bootstrap.graph.module_import_key.migrated", `"unkeyed_remaining":0`, `"repointed_imports":0`, `"imports_on_duplicates":0`} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("logs missing %s", want)
		}
	}
	// moved_imports counted the edges matched on the duplicates, not the edges
	// created, so it overstated the move (95,000 logged, 5,000 created).
	if strings.Contains(logs.String(), "moved_imports") {
		t.Errorf("logs still carry the misleading moved_imports field: %s", logs.String())
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
		// The keeper also takes the group's language, so a legacy node written
		// with no lang ends with lang = '' like the node the writer would mint.
		"SET keeper.import_key = name + '@' + lang, keeper.lang = lang",
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
