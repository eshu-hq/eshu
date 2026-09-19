// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestInfraResourceEntitiesMigrationShape pins the #6793 read-model table:
// entity grain keyed by entity_id, the (repo_id, relative_path) cleanup key the
// derive step deletes by, the dimension columns the aggregate readers group on,
// and the backfill marker table readers consult before trusting the table.
func TestInfraResourceEntitiesMigrationShape(t *testing.T) {
	t.Parallel()

	sql := MigrationSQL("infra_resource_entities")
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS infra_resource_entities",
		"entity_id         TEXT PRIMARY KEY",
		"repo_id",
		"scope_id",
		"generation_id",
		"relative_path",
		"label",
		"entity_name",
		"kind",
		"resource_type",
		"data_type",
		"provider",
		"environment",
		"resource_service",
		"resource_category",
		"service_kind",
		"CREATE INDEX IF NOT EXISTS infra_resource_entities_repo_path_idx",
		"ON infra_resource_entities (repo_id, relative_path)",
		"CREATE TABLE IF NOT EXISTS infra_resource_entity_backfill_markers",
		"marker_name  TEXT PRIMARY KEY",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("infra_resource_entities migration missing %q:\n%s", want, sql)
		}
	}
	// The (label, entity_name, entity_id) keyset index serves only the
	// follow-up /api/v0/iac/resources read; it lands with that read and its
	// plan evidence, not ahead of it on the projector write path.
	if strings.Contains(sql, "infra_resource_entities_label_name_idx") {
		t.Fatal("migration must not create an index no reader in this change uses")
	}
	if strings.Contains(sql, "INSERT INTO infra_resource_entities") {
		t.Fatal("migration must create the empty table only; backfill runs per repo under the derive lock")
	}
}
