// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestIaCInventoryIndexIsPartialAndCurrentReadAligned(t *testing.T) {
	t.Parallel()

	sql := MigrationSQL("iac_active_inventory_index")
	for _, want := range []string{
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS",
		"fact_records_iac_active_inventory_idx",
		"scope_id",
		"generation_id",
		"(payload->>'entity_type')",
		"(payload->>'entity_name')",
		"(payload->>'entity_id')",
		"fact_kind = 'content_entity'",
		"is_tombstone = FALSE",
		"TerraformResource",
		"TerraformModule",
		"TerraformDataSource",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("IaC inventory index SQL missing %q:\n%s", want, sql)
		}
	}
}
