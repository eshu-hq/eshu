// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"os"
	"strings"
	"testing"
)

func TestSupplyChainRuntimeFilterIndexesStayInBootstrapAndMigration(t *testing.T) {
	t.Parallel()

	var migrationSQL strings.Builder
	for _, path := range []string{
		"migrations/079_supply_chain_runtime_filter_workload_index.sql",
		"migrations/080_supply_chain_runtime_filter_entity_keys_index.sql",
	} {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read runtime-filter index migration %q: %v", path, err)
		}
		migrationSQL.Write(contents)
	}

	for _, want := range []string{
		"fact_records_workload_identity_workload_idx",
		"fact_records_workload_identity_entity_keys_idx",
		"payload->>'workload_id'",
		"USING GIN ((payload->'entity_keys'))",
		"WHERE fact_kind = 'reducer_workload_identity'",
		"AND is_tombstone = FALSE",
	} {
		if !strings.Contains(factRecordSchemaSQL, want) {
			t.Fatalf("bootstrap schema missing runtime-filter index marker %q", want)
		}
		if !strings.Contains(migrationSQL.String(), want) {
			t.Fatalf("migration missing runtime-filter index marker %q", want)
		}
	}

	if strings.Count(migrationSQL.String(), "CREATE INDEX CONCURRENTLY IF NOT EXISTS") != 2 {
		t.Fatal("runtime-filter migration must avoid blocking fact-record writes")
	}
}

func TestSupplyChainRuntimeFilterNormalizedFallbackIndexesStayInBootstrapAndMigrations(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		migration string
		index     string
		key       string
		kind      string
	}{
		{"211_supply_chain_identity_workload_trim_index.sql", "fact_records_workload_identity_workload_trim_idx", "BTRIM(payload->>'workload_id')", "reducer_workload_identity"},
		{"212_supply_chain_identity_entity_trim_index.sql", "fact_records_workload_identity_entity_trim_idx", "ON fact_records (fact_id)", "reducer_workload_identity"},
		{"213_supply_chain_catalog_workload_trim_index.sql", "fact_records_service_catalog_workload_trim_idx", "BTRIM(payload->>'workload_id')", "reducer_service_catalog_correlation"},
		{"214_supply_chain_catalog_service_trim_index.sql", "fact_records_service_catalog_service_trim_idx", "BTRIM(payload->>'service_id')", "reducer_service_catalog_correlation"},
		{"215_supply_chain_cicd_environment_trim_index.sql", "fact_records_ci_cd_environment_trim_idx", "BTRIM(payload->>'environment')", "reducer_ci_cd_run_correlation"},
	} {
		t.Run(tc.index, func(t *testing.T) {
			contents, err := os.ReadFile("migrations/" + tc.migration)
			if err != nil {
				t.Fatalf("read migration %s: %v", tc.migration, err)
			}
			migration := string(contents)
			for _, want := range []string{tc.index, tc.key, "fact_kind = '" + tc.kind + "'", "is_tombstone = FALSE"} {
				if !strings.Contains(migration, want) {
					t.Errorf("migration %s missing %q", tc.migration, want)
				}
				if !strings.Contains(factRecordSchemaSQL, want) {
					t.Errorf("bootstrap schema missing %q for %s", want, tc.index)
				}
			}
			if strings.Count(migration, "CREATE INDEX CONCURRENTLY IF NOT EXISTS") != 1 {
				t.Errorf("migration %s must contain exactly one concurrent index build", tc.migration)
			}
		})
	}
}
