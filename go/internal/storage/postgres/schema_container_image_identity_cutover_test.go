// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestContainerImageIdentityCutoverMigrationMatchesBootstrapMirror(t *testing.T) {
	t.Parallel()

	var migration Definition
	for _, definition := range BootstrapDefinitions() {
		if definition.Name == "container_image_identity_cutover_guard" {
			migration = definition
			break
		}
	}
	if migration.Name == "" {
		t.Fatal("container image identity cutover migration is missing")
	}
	if got, want := strings.TrimSpace(migration.SQL), strings.TrimSpace(containerImageIdentityCutoverSchemaSQL); got != want {
		t.Fatal("container image identity cutover migration and bootstrap mirror differ")
	}
}

func TestContainerImageIdentityCutoverSchemaCarriesCompatibilityFence(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS container_image_identity_cutovers",
		"PRIMARY KEY (scope_id, generation_id)",
		"current_setting('transaction_isolation')",
		"legacy container image identity writes require read committed isolation",
		"pg_advisory_xact_lock(",
		"hashtextextended(NEW.scope_id || E'\\x1f' || NEW.generation_id, 5854)",
		"BEFORE INSERT ON fact_records",
		"NEW.fact_kind = 'reducer_container_image_identity'",
		"COALESCE(NEW.payload->>'identity_format', '') <> 'image_ref_v2'",
		"RETURN NULL",
	} {
		if !strings.Contains(containerImageIdentityCutoverSchemaSQL, want) {
			t.Fatalf("container image identity cutover schema missing %q", want)
		}
	}
	if got, want := strings.Count(
		containerImageIdentityCutoverSchemaSQL,
		"CREATE TRIGGER fact_records_legacy_container_image_identity_cutover_guard",
	), 1; got != want {
		t.Fatalf("cutover trigger definitions = %d, want %d", got, want)
	}
}
