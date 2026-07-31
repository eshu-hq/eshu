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
		"activated_by_work_item_id TEXT NOT NULL",
		"activated_by_claim_epoch BIGINT NOT NULL",
		"current_setting('transaction_isolation')",
		"legacy container image identity writes require read committed isolation",
		"guard_legacy_container_image_identity_statement()",
		"SELECT DISTINCT scope_id, generation_id",
		"min(scope_id)",
		"min(generation_id)",
		"count(*)",
		"LIMIT 2",
		"IF legacy_key_count = 1 THEN",
		"legacy container image identity writer statement spans multiple scope generations",
		"legacy container image identity writer is incompatible with completed image_ref_v2 cutover",
		"pg_advisory_xact_lock(",
		"NEW.scope_id || E'\\x1f' || NEW.generation_id",
		"CREATE OR REPLACE FUNCTION guard_container_image_identity_cutover_marker()",
		"container_image_identity_v2_required",
		"BOOLEAN NOT NULL DEFAULT FALSE",
		"container_image_identity_claim_epoch BIGINT NOT NULL DEFAULT 0",
		"container_image_identity_v2_authorized_status\n            TEXT NOT NULL DEFAULT ''",
		"fact_work_items_container_image_identity_v2_status_check",
		"NOT container_image_identity_v2_required",
		"status = container_image_identity_v2_authorized_status",
		"CREATE OR REPLACE FUNCTION advance_container_image_identity_claim_epoch()",
		"NEW.container_image_identity_claim_epoch =\n                OLD.container_image_identity_claim_epoch",
		"NEW.container_image_identity_claim_epoch :=\n                    OLD.container_image_identity_claim_epoch + 1",
		"legacy container image identity claim is incompatible with completed image_ref_v2 cutover",
		"container image identity claim epoch must advance exactly once",
		"IF OLD.container_image_identity_v2_required THEN",
		"BEFORE UPDATE OF\n            last_attempt_at,\n            container_image_identity_claim_epoch",
		"WHEN (\n            OLD.domain = 'container_image_identity'\n            AND (\n                OLD.container_image_identity_v2_required\n                OR NEW.container_image_identity_claim_epoch <>\n                    OLD.container_image_identity_claim_epoch + 1\n            )\n        )",
		"SET status = 'running'",
		"container_image_identity_v2_required = TRUE",
		"container_image_identity_v2_authorized_status = 'running'",
		"work_item.container_image_identity_claim_epoch = NEW.activated_by_claim_epoch",
		"GET DIAGNOSTICS work_item_count = ROW_COUNT",
		"container image identity first cutover requires the exact active claim epoch",
		"existing container image identity cutover has invalid queue fence state",
		"BEFORE INSERT ON container_image_identity_cutovers",
		"AFTER UPDATE ON fact_records",
		"REFERENCING NEW TABLE AS updated_rows",
		"AFTER INSERT ON fact_records",
		"REFERENCING NEW TABLE AS inserted_rows",
		"fact_records_container_image_identity_legacy_cleanup_idx",
		"ON fact_records (scope_id, generation_id, fact_id)",
		"COALESCE(payload->>'identity_format', '') <> 'image_ref_v2'",
	} {
		if !strings.Contains(containerImageIdentityCutoverSchemaSQL, want) {
			t.Fatalf("container image identity cutover schema missing %q", want)
		}
	}
	if got, want := strings.Count(
		containerImageIdentityCutoverSchemaSQL,
		"CREATE TRIGGER fact_records_legacy_container_image_identity_cutover_guard",
	), 2; got != want {
		t.Fatalf("cutover trigger definitions = %d, want %d", got, want)
	}
	if got, want := strings.Count(
		containerImageIdentityCutoverSchemaSQL,
		"CREATE TRIGGER container_image_identity_cutover_marker_guard",
	), 1; got != want {
		t.Fatalf("cutover marker trigger definitions = %d, want %d", got, want)
	}
	if got, want := strings.Count(
		containerImageIdentityCutoverSchemaSQL,
		"CREATE TRIGGER fact_work_items_container_image_identity_claim_epoch_advance",
	), 1; got != want {
		t.Fatalf("claim epoch trigger definitions = %d, want %d", got, want)
	}
	for _, forbidden := range []string{
		"CREATE OR REPLACE FUNCTION guard_container_image_identity_ack",
		"CREATE TRIGGER fact_work_items_container_image_identity_ack_guard",
		"eshu_internal.container_image_identity_ack_v1",
		"CREATE OR REPLACE FUNCTION guard_legacy_container_image_identity_fact()",
		"pg_try_advisory_xact_lock(",
		"legacy container image identity writer overlapped image_ref_v2 cutover",
		"eshu_internal.container_image_identity_cutover_v1",
	} {
		if strings.Contains(containerImageIdentityCutoverSchemaSQL, forbidden) {
			t.Fatalf("cutover schema retained rejected ACK trigger surface %q", forbidden)
		}
	}
}
