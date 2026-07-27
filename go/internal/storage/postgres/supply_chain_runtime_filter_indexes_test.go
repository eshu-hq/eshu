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
