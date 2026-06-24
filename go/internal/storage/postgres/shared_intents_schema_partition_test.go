// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestSharedIntentSchemaSQLIncludesPartitionCandidateIndexes(t *testing.T) {
	t.Parallel()

	sqlStr := SharedIntentSchemaSQL()
	for _, want := range []string{
		"ADD COLUMN IF NOT EXISTS partition_hash",
		"ADD COLUMN IF NOT EXISTS is_refresh_intent",
		"GENERATED ALWAYS AS (COALESCE(payload->>'action' = 'refresh', false)) STORED",
		"shared_projection_intents_domain_partition_pending_idx",
		"shared_projection_intents_domain_partition_refresh_primary_idx",
		"shared_projection_intents_domain_unhashed_refresh_primary_idx",
		"is_refresh_intent DESC",
	} {
		if !strings.Contains(sqlStr, want) {
			t.Fatalf("schema SQL missing %q", want)
		}
	}
}
