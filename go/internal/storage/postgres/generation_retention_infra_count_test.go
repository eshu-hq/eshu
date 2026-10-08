// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestGenerationRetentionRowCountsIncludeInfraMirrorRows pins that the
// admission count has an infra_resource_entities arm, so a generation's
// prospective mirror-row deletes count against BatchRowLimit like its
// content_entities deletes.
func TestGenerationRetentionRowCountsIncludeInfraMirrorRows(t *testing.T) {
	t.Parallel()

	if !strings.Contains(generationRetentionRowCountsQuery,
		"SELECT candidate.generation_id, 'infra_resource_entities'") {
		t.Fatalf("row-count query has no infra_resource_entities arm:\n%s", generationRetentionRowCountsQuery)
	}
}

// TestGenerationRetentionInfraMirrorCountLive was retired in #7695. It ran
// against a hand-built schema that drifted from the migrations (missing
// scope_id on fact_records) and was enrolled in no CI lane, so the drift went
// unnoticed. Its unique case — a mirror row whose entity a retained
// generation still holds is neither counted nor pruned — now lives in
// TestGenerationRetentionPrunesMigratedSchemaLive (the entity-3 fixture),
// which runs against the real bootstrap schema in the blocking
// reducer-contention gate. This file keeps only the hermetic arm-shape test.
