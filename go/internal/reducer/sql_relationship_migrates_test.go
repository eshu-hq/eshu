// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// sqlMigrationTarget builds one migration_targets metadata row in the
// []any-of-map[string]any shape a JSON-decoded facts envelope produces
// (mirrors the "source_tables": []any{...} convention used elsewhere in this
// test package for #5345 source_tables metadata).
func sqlMigrationTarget(kind, name, operation string, lineNumber int) map[string]any {
	return map[string]any{
		"kind":        kind,
		"name":        name,
		"operation":   operation,
		"line_number": lineNumber,
	}
}

// TestExtractSQLRelationshipRowsMigratesResolvesTarget is the #5346
// failing-then-green proof: a SqlMigration entity whose migration_targets
// metadata names an existing SqlTable produces a MIGRATES edge resolved
// directly against that table, with source_path anchored to the migration
// file (mirrors READS_FROM/TRIGGERS/EXECUTES/INDEXES resolution, #5345/#5330).
// A drop operation stays metadata-only: MIGRATES records adjacency/provenance
// without encoding target head-state presence or absence on the edge.
func TestExtractSQLRelationshipRowsMigratesResolvesTarget(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		sqlRelationshipRepositoryEnvelope(false, nil),
		sqlRelationshipContentEntity("content-entity:e_tbl1", "SqlTable", "public.users", "db/schema.sql", nil),
		sqlRelationshipContentEntity("content-entity:e_mig1", "SqlMigration", "V1__add_users", "db/migrations/V1__add_users.sql", map[string]any{
			"sql_entity_type":   "SqlMigration",
			"tool":              "flyway",
			"migration_targets": []any{sqlMigrationTarget("SqlTable", "public.users", "drop", 1)},
		}),
	}

	_, rows, stats := ExtractSQLRelationshipRows(envelopes)

	if stats.UnresolvedMigrationTargets != 0 {
		t.Fatalf("UnresolvedMigrationTargets = %d, want 0", stats.UnresolvedMigrationTargets)
	}
	if stats.AmbiguousMigrationTargets != 0 {
		t.Fatalf("AmbiguousMigrationTargets = %d, want 0", stats.AmbiguousMigrationTargets)
	}

	var found bool
	for _, row := range rows {
		if anyToString(row["relationship_type"]) != "MIGRATES" {
			continue
		}
		found = true
		if got, want := row["source_entity_id"], "content-entity:e_mig1"; got != want {
			t.Errorf("source_entity_id = %v, want %v", got, want)
		}
		if got, want := row["target_entity_id"], "content-entity:e_tbl1"; got != want {
			t.Errorf("target_entity_id = %v, want %v", got, want)
		}
		if got, want := row["source_entity_type"], "SqlMigration"; got != want {
			t.Errorf("source_entity_type = %v, want %v", got, want)
		}
		if got, want := row["target_entity_type"], "SqlTable"; got != want {
			t.Errorf("target_entity_type = %v, want %v", got, want)
		}
		if got, want := row["source_path"], "/repo/db/migrations/V1__add_users.sql"; got != want {
			t.Errorf("source_path = %v, want %v", got, want)
		}
		if _, ok := row["operation"]; ok {
			t.Errorf("MIGRATES row unexpectedly carries metadata-only operation: %#v", row)
		}
	}
	if !found {
		t.Fatalf("no MIGRATES row in %#v", rows)
	}
}

// TestExtractSQLRelationshipRowsMigratesResolvesQualifiedTargetAgainstBareEntity
// is the #5346 codex regression: a migration that names a schema-qualified
// target (ALTER TABLE public.orders) must still resolve against a bare
// canonical definition (CREATE TABLE orders) via the unqualified-name fallback,
// exactly as the READS_FROM resolver does. Before the fallback, the exact-name
// lookup returned no candidates and the MIGRATES edge was silently skipped as
// unresolved for the very common mixed qualified/unqualified SQL.
func TestExtractSQLRelationshipRowsMigratesResolvesQualifiedTargetAgainstBareEntity(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		sqlRelationshipRepositoryEnvelope(false, nil),
		// Canonical table defined bare (CREATE TABLE orders).
		sqlRelationshipContentEntity("content-entity:e_ord", "SqlTable", "orders", "db/schema.sql", nil),
		// Migration targets it schema-qualified (ALTER TABLE public.orders).
		sqlRelationshipContentEntity("content-entity:e_mig2", "SqlMigration", "V2__alter_orders", "db/migrations/V2__alter_orders.sql", map[string]any{
			"sql_entity_type":   "SqlMigration",
			"tool":              "flyway",
			"migration_targets": []any{sqlMigrationTarget("SqlTable", "public.orders", "alter", 1)},
		}),
	}

	_, rows, stats := ExtractSQLRelationshipRows(envelopes)

	if stats.UnresolvedMigrationTargets != 0 {
		t.Fatalf("UnresolvedMigrationTargets = %d, want 0 (qualified target must resolve against the bare entity)", stats.UnresolvedMigrationTargets)
	}
	if stats.AmbiguousMigrationTargets != 0 {
		t.Fatalf("AmbiguousMigrationTargets = %d, want 0", stats.AmbiguousMigrationTargets)
	}

	var found bool
	for _, row := range rows {
		if anyToString(row["relationship_type"]) != "MIGRATES" {
			continue
		}
		found = true
		if got, want := row["source_entity_id"], "content-entity:e_mig2"; got != want {
			t.Errorf("source_entity_id = %v, want %v", got, want)
		}
		if got, want := row["target_entity_id"], "content-entity:e_ord"; got != want {
			t.Errorf("target_entity_id = %v, want %v (bare orders)", got, want)
		}
	}
	if !found {
		t.Fatalf("no MIGRATES row (qualified target did not resolve against the bare entity) in %#v", rows)
	}
}

// TestExtractSQLRelationshipRowsMigratesSkipsAmbiguousSameNameTarget guards
// the #5346 "never guess" trap: a repo with two same-kind, same-name SqlTable
// entities (e.g. schema.sql and a legacy schema file both defining "users",
// neither the migration's own file) must skip the MIGRATES edge rather than
// resolve to an arbitrary one, and must count it as ambiguous, not
// unresolved, so an operator can distinguish "never existed" from "matched
// more than one" (mirrors resolveSQLReadTarget's SqlTable/SqlView ambiguity
// for READS_FROM, #5345).
func TestExtractSQLRelationshipRowsMigratesSkipsAmbiguousSameNameTarget(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		sqlRelationshipRepositoryEnvelope(false, nil),
		sqlRelationshipContentEntity("content-entity:e_tbl1", "SqlTable", "public.users", "db/schema.sql", nil),
		sqlRelationshipContentEntity("content-entity:e_tbl2", "SqlTable", "public.users", "db/legacy_schema.sql", nil),
		sqlRelationshipContentEntity("content-entity:e_mig1", "SqlMigration", "V1__add_users", "db/migrations/V1__add_users.sql", map[string]any{
			"sql_entity_type":   "SqlMigration",
			"tool":              "flyway",
			"migration_targets": []any{sqlMigrationTarget("SqlTable", "public.users", "alter", 1)},
		}),
	}

	_, rows, stats := ExtractSQLRelationshipRows(envelopes)

	if stats.AmbiguousMigrationTargets != 1 {
		t.Fatalf("AmbiguousMigrationTargets = %d, want 1", stats.AmbiguousMigrationTargets)
	}
	if stats.UnresolvedMigrationTargets != 0 {
		t.Fatalf("UnresolvedMigrationTargets = %d, want 0 (ambiguous, not unresolved)", stats.UnresolvedMigrationTargets)
	}
	for _, row := range rows {
		if anyToString(row["relationship_type"]) == "MIGRATES" {
			t.Fatalf("unexpected MIGRATES row for an ambiguous same-name target: %#v", row)
		}
	}
}

// TestExtractSQLRelationshipRowsMigratesSkipsUnresolvedTarget proves a
// migration_targets entry naming a table that does not exist anywhere in the
// repo is counted as unresolved (not ambiguous) and produces no edge.
func TestExtractSQLRelationshipRowsMigratesSkipsUnresolvedTarget(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		sqlRelationshipRepositoryEnvelope(false, nil),
		sqlRelationshipContentEntity("content-entity:e_mig1", "SqlMigration", "V1__add_users", "db/migrations/V1__add_users.sql", map[string]any{
			"sql_entity_type":   "SqlMigration",
			"tool":              "flyway",
			"migration_targets": []any{sqlMigrationTarget("SqlTable", "public.ghost", "alter", 1)},
		}),
	}

	_, rows, stats := ExtractSQLRelationshipRows(envelopes)

	if stats.UnresolvedMigrationTargets != 1 {
		t.Fatalf("UnresolvedMigrationTargets = %d, want 1", stats.UnresolvedMigrationTargets)
	}
	if stats.AmbiguousMigrationTargets != 0 {
		t.Fatalf("AmbiguousMigrationTargets = %d, want 0 (unresolved, not ambiguous)", stats.AmbiguousMigrationTargets)
	}
	for _, row := range rows {
		if anyToString(row["relationship_type"]) == "MIGRATES" {
			t.Fatalf("unexpected MIGRATES row for an unresolved target: %#v", row)
		}
	}
}

// TestSQLRelationshipHandlerEmitsMigratesIntent proves the full materialization
// handler promotes a resolved MIGRATES row to a durable shared-projection
// intent, mirroring TestSQLRelationshipHandlerEmitsIntents.
func TestSQLRelationshipHandlerEmitsMigratesIntent(t *testing.T) {
	t.Parallel()

	writer := &recordingSQLRelationshipIntentWriter{}
	handler := SQLRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{
			envelopes: []facts.Envelope{
				sqlRelationshipRepositoryEnvelope(false, nil),
				sqlRelationshipContentEntity("content-entity:e_tbl1", "SqlTable", "public.users", "db/schema.sql", nil),
				sqlRelationshipContentEntity("content-entity:e_mig1", "SqlMigration", "V1__add_users", "db/migrations/V1__add_users.sql", map[string]any{
					"sql_entity_type":   "SqlMigration",
					"tool":              "flyway",
					"migration_targets": []any{sqlMigrationTarget("SqlTable", "public.users", "create", 1)},
				}),
			},
		},
		IntentWriter: writer,
	}

	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	intent := Intent{
		IntentID:     "intent-sql-rel-migrates",
		ScopeID:      "scope-db",
		GenerationID: "gen-1",
		SourceSystem: "git",
		Domain:       DomainSQLRelationshipMaterialization,
		EntityKeys:   []string{"repo-123"},
		EnqueuedAt:   now,
		AvailableAt:  now,
		Status:       IntentStatusPending,
	}

	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("result.Status = %q, want %q", result.Status, ResultStatusSucceeded)
	}

	edges := writer.edgeRows()
	if len(edges) != 1 {
		t.Fatalf("per-edge intents = %d, want 1", len(edges))
	}
	if got := anyToString(edges[0].Payload["relationship_type"]); got != "MIGRATES" {
		t.Fatalf("relationship_type = %q, want %q", got, "MIGRATES")
	}
	if !rowUsesRefreshFence(edges[0]) {
		t.Fatalf("MIGRATES edge intent not marked retract_via_refresh")
	}
}
