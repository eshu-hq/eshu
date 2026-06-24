// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestBootstrapDefinitionsIncludeServiceCatalogCorrelationFactIndexes(t *testing.T) {
	t.Parallel()

	var indexes Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "service_catalog_fact_record_indexes" {
			indexes = def
			break
		}
	}
	if indexes.Name == "" {
		t.Fatal("service_catalog_fact_record_indexes definition missing")
	}
	for _, want := range []string{
		"fact_records_service_catalog_correlations_entity_idx",
		"fact_records_service_catalog_correlations_repository_idx",
		"fact_records_service_catalog_correlations_service_idx",
		"fact_records_service_catalog_correlations_candidate_repository_idx",
		"fact_records_service_catalog_correlations_owner_idx",
		"fact_records_service_catalog_correlations_workload_idx",
		"'reducer_service_catalog_correlation'",
		"(payload->>'provider')",
		"(payload->>'entity_ref')",
		"(payload->>'repository_id')",
		"(payload->'candidate_repository_ids')",
		"(payload->>'service_id')",
		"(payload->>'workload_id')",
		"(payload->>'owner_ref')",
		"(payload->>'outcome')",
		"(payload->>'drift_status')",
		"fact_id ASC",
	} {
		if !strings.Contains(indexes.SQL, want) {
			t.Fatalf("service catalog fact index SQL missing %q", want)
		}
	}
}
