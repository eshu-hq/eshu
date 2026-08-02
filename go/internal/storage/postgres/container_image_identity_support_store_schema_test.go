// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestContainerImageIdentitySupportStoreSchemaContract(t *testing.T) {
	t.Parallel()

	sql := MigrationSQL("container_image_identity_support_store") +
		MigrationSQL("container_image_identity_support_current_view") +
		MigrationSQL("container_image_identity_current_facts_function") +
		MigrationSQL("container_image_identity_current_support_facts_function")
	want := []string{
		"CREATE SEQUENCE IF NOT EXISTS container_image_identity_activation_epoch_seq",
		"CREATE TABLE IF NOT EXISTS container_image_identity_support_sets",
		"set_id BYTEA PRIMARY KEY CHECK (octet_length(set_id) = 32)",
		"content_hash BYTEA NOT NULL CHECK (octet_length(content_hash) = 32)",
		"UNIQUE (scope_id, content_hash)",
		"CREATE TABLE IF NOT EXISTS container_image_identity_supports",
		"support_id BYTEA NOT NULL CHECK (octet_length(support_id) = 32)",
		"PRIMARY KEY (set_id, digest, support_id)",
		"CREATE INDEX IF NOT EXISTS container_image_identity_supports_image_ref_idx\n    ON container_image_identity_supports (set_id, image_ref, digest, support_id)",
		"CREATE INDEX IF NOT EXISTS container_image_identity_supports_repository_idx\n    ON container_image_identity_supports (set_id, repository_id, digest, support_id)",
		"CREATE INDEX IF NOT EXISTS container_image_identity_supports_outcome_idx\n    ON container_image_identity_supports (set_id, outcome, digest, support_id)",
		"USING GIN (source_repository_ids)",
		"CREATE TABLE IF NOT EXISTS container_image_identity_scope_state",
		"container_image_identity_v3_required",
		"container_image_identity_v3_authorized_status",
		"fact_work_items_container_image_identity_v3_status_check",
		"CREATE OR REPLACE FUNCTION guard_container_image_identity_cutover_marker()",
		"WHEN work_item.container_image_identity_v3_required THEN 'running'",
		"OR work_item.container_image_identity_v3_authorized_status = work_item.status",
		"active_generation_id TEXT",
		"activation_epoch BIGINT NOT NULL",
		"active_set_id BYTEA",
		"last_set_id BYTEA",
		"last_set_hash BYTEA CHECK (last_set_hash IS NULL OR octet_length(last_set_hash) = 32)",
		"published_claim_epoch BIGINT NOT NULL DEFAULT 0",
		"CREATE OR REPLACE FUNCTION reset_container_image_identity_scope_state()",
		"AFTER INSERT OR UPDATE OF active_generation_id ON ingestion_scopes",
		"active_set_id = NULL",
		"nextval('container_image_identity_activation_epoch_seq')",
		"CREATE OR REPLACE VIEW container_image_identity_current_supports",
		"generation.status = 'active'",
		"'reducer_container_image_identity:' ||",
		"sha256(",
		"'canonical:container_image_identity:' || support.digest AS canonical_id",
		"CREATE TABLE IF NOT EXISTS container_image_identity_storage_cutover",
		"VALIDATE CONSTRAINT fact_work_items_container_image_identity_v3_status_check",
		"CREATE OR REPLACE FUNCTION reject_container_image_identity_fact_record_write()",
		"state.active_set_id IS NOT NULL",
		"work_item.container_image_identity_v3_required",
		"WHERE state.active_set_id IS NULL",
		"support.set_id = state.active_set_id",
		"AND support.digest = selected.digest",
		"CREATE OR REPLACE FUNCTION container_image_identity_current_facts_for(",
		"CREATE OR REPLACE FUNCTION container_image_identity_current_support_facts_for(",
		"CREATE OR REPLACE FUNCTION container_image_identity_try_decode_utf8_hex(value TEXT)",
		"cardinality(parts.parts) = 4",
		"decoded.support_id IS NOT NULL",
		"octet_length(decoded.support_id) = 32 AS is_canonical",
		"NOT cursor.is_canonical",
		"'reducer_container_image_identity_support:' ||",
		"support.support_id",
		"cursor_boundary AS MATERIALIZED",
		"v3_candidates AS MATERIALIZED",
		"legacy_candidates AS MATERIALIZED",
		"ORDER BY\n        convert_to(state.scope_id, 'UTF8'),",
		"after_fact_id TEXT",
		"result_limit INTEGER",
		"SECURITY INVOKER",
		"selected_digests AS MATERIALIZED",
		"identified.identity_id > $6",
		"LIMIT GREATEST($7, 0)",
		"selected_supports AS MATERIALIZED",
		"FROM selected_supports AS support",
		"source_repositories AS MATERIALIZED",
		"build_repositories AS MATERIALIZED",
		"missing_evidence AS MATERIALIZED",
		"GROUP BY row.digest",
		"support.scope_id,\n                support.support_id",
	}
	for _, fragment := range want {
		if !strings.Contains(sql, fragment) {
			t.Errorf("support-store migration missing %q", fragment)
		}
	}

	for _, forbidden := range []string{
		"container_image_identity_supports_digest_idx",
		"REFERENCES fact_records",
		"CREATE OR REPLACE VIEW container_image_identity_current_facts\n",
		"FROM ranked AS row WHERE row.digest = digest.digest",
		"matched_supports AS MATERIALIZED",
		"jsonb_agg(to_jsonb(",
		"DELETE FROM fact_records\n    WHERE fact_kind = 'reducer_container_image_identity'",
		"DROP INDEX IF EXISTS fact_records_container_image_identity_",
		"DROP TRIGGER IF EXISTS fact_records_reject_container_image_identity_v2",
		"DROP TRIGGER IF EXISTS ingestion_scopes_container_image_identity_state_reset",
	} {
		if strings.Contains(sql, forbidden) {
			t.Errorf("support-store migration contains forbidden %q", forbidden)
		}
	}
}

func TestContainerImageIdentitySupportStoreCutoverDefinitionsStayAdjacent(t *testing.T) {
	t.Parallel()

	defs := BootstrapDefinitions()
	lastCutover := -1
	for index, def := range defs {
		if def.Name == "container_image_identity_current_support_facts_function" {
			lastCutover = index
			break
		}
	}
	if lastCutover < 0 {
		t.Fatal("container image identity cutover definition missing")
	}
	if lastCutover+1 >= len(defs) {
		t.Fatal("cross-scope completion queue must follow the identity cutover")
	}
	if got := defs[lastCutover+1].Name; got != "cross_scope_completion_queue" {
		t.Fatalf("definition after identity cutover = %q, want cross_scope_completion_queue", got)
	}
}

func TestContainerImageIdentitySupportPaginationUsesBytewiseFactIDOrder(t *testing.T) {
	t.Parallel()

	sql := MigrationSQL("container_image_identity_current_support_facts_function")
	for _, want := range []string{
		"convert_to(state.scope_id, 'UTF8')",
		"convert_to(support.digest, 'UTF8')",
		"convert_to(fact.payload->>'digest', 'UTF8')",
		"convert_to(cursor.scope_id, 'UTF8')",
		"convert_to(cursor.digest, 'UTF8')",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("support pagination migration missing bytewise key %q:\n%s", want, sql)
		}
	}
	for _, forbidden := range []string{
		"(state.scope_id, support.digest, support.support_id) >",
		"ORDER BY state.scope_id, support.digest, support.support_id",
		"(state.scope_id, fact.payload->>'digest', sha256(convert_to(fact.fact_id, 'UTF8'))) >",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("support pagination migration retains collation-sensitive key %q:\n%s", forbidden, sql)
		}
	}
	executableQuery := sqlWithoutLineComments(listActiveSupplyChainImpactFactsQuery)
	for _, want := range []string{
		"convert_to(fact.fact_id, 'UTF8')",
		"convert_to($11, 'UTF8')",
	} {
		if !strings.Contains(executableQuery, want) {
			t.Fatalf("supply-chain caller missing bytewise cursor key %q:\n%s", want, executableQuery)
		}
	}
}

func TestContainerImageIdentityCutoverFilesAreAdjacentAndFailClosed(t *testing.T) {
	t.Parallel()

	defs := BootstrapDefinitions()
	if len(defs) < 4 {
		t.Fatalf("bootstrap definitions = %d, want at least 4", len(defs))
	}
	want := []string{
		"container_image_identity_support_store",
		"container_image_identity_support_current_view",
		"container_image_identity_current_facts_function",
		"container_image_identity_current_support_facts_function",
	}
	start := -1
	for index, def := range defs {
		if def.Name == want[0] {
			start = index
			break
		}
	}
	if start < 0 || start+len(want) > len(defs) {
		t.Fatal("container image identity cutover definitions missing")
	}
	for i, name := range want {
		if got := defs[start+i].Name; got != name {
			t.Fatalf("cutover definition %d = %q, want %q", i, got, name)
		}
	}
	guard := MigrationSQL("container_image_identity_support_store")
	marker := strings.Index(guard, "INSERT INTO container_image_identity_storage_cutover")
	reject := strings.Index(guard, "CREATE TRIGGER fact_records_reject_container_image_identity_v2")
	if marker < 0 || reject < marker {
		t.Fatalf("capability marker/rolling guard order is not fail-closed:\n%s", guard)
	}
	view := MigrationSQL("container_image_identity_support_current_view")
	if !strings.Contains(view, "WHERE state.active_set_id IS NULL") ||
		strings.Contains(view, "INSERT INTO container_image_identity_support_sets") ||
		strings.Contains(view, "INSERT INTO container_image_identity_supports") ||
		strings.Contains(view, "DELETE FROM fact_records") {
		t.Fatalf("092a must retain legacy rows behind a read-only per-scope authority switch:\n%s", view)
	}
}
