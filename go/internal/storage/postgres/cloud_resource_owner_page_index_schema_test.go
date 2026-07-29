// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestCloudResourceOwnerPageIndexesMigration(t *testing.T) {
	t.Parallel()

	var sql strings.Builder
	for _, name := range []string{
		"cloud_resource_owner_page_index",
		"cloud_resource_owner_provider_page_index",
		"cloud_resource_owner_region_page_index",
		"cloud_resource_owner_account_page_index",
		"cloud_resource_owner_runtime_digest_index",
	} {
		migration := MigrationSQL(name)
		if strings.TrimSpace(migration) == "" {
			t.Fatalf("%s migration is missing", name)
		}
		sql.WriteString(migration)
	}
	for _, want := range []string{
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS graph_node_owner_cloud_resource_page_idx",
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS graph_node_owner_cloud_resource_provider_page_idx",
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS graph_node_owner_cloud_resource_region_page_idx",
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS graph_node_owner_cloud_resource_account_page_idx",
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS graph_node_owner_cloud_resource_runtime_digest_idx",
		"((winning_row->>'resource_type')), uid",
		"((winning_row->>'collector_kind')), ((winning_row->>'resource_type')), uid",
		"((winning_row->>'region')), ((winning_row->>'resource_type')), uid",
		"((winning_row->>'account_id')), ((winning_row->>'resource_type')), uid",
		"((winning_row->>'running_image_digest')), ((winning_row->>'arn')), uid",
	} {
		if !strings.Contains(sql.String(), want) {
			t.Errorf("migration missing %q:\n%s", want, sql.String())
		}
	}
	if strings.Contains(sql.String(), "INCLUDE (winning_row)") {
		t.Fatal("migration must not duplicate every winning_row JSONB value into the page indexes")
	}
	runtimeDigestMigration := MigrationSQL("cloud_resource_owner_runtime_digest_index")
	for _, want := range []string{
		"NULLIF(BTRIM(winning_row->>'running_image_digest'), '') IS NOT NULL",
		"NULLIF(BTRIM(winning_row->>'arn'), '') IS NOT NULL",
	} {
		if !strings.Contains(runtimeDigestMigration, want) {
			t.Errorf("runtime digest migration missing selective predicate %q:\n%s", want, runtimeDigestMigration)
		}
	}
	for _, broad := range []string{
		"winning_row->>'running_image_digest' IS NOT NULL",
		"winning_row->>'arn' IS NOT NULL",
	} {
		if strings.Contains(runtimeDigestMigration, broad) {
			t.Errorf("runtime digest migration uses broad predicate %q:\n%s", broad, runtimeDigestMigration)
		}
	}
}

func TestBootstrapDefinitionsDoNotBundleConcurrentIndexStatements(t *testing.T) {
	t.Parallel()

	for _, definition := range BootstrapDefinitions() {
		if count := strings.Count(definition.SQL, "CREATE INDEX CONCURRENTLY"); count > 1 {
			t.Errorf(
				"bootstrap definition %q bundles %d concurrent indexes in one ExecContext call; want at most 1",
				definition.Name,
				count,
			)
		}
	}
}
