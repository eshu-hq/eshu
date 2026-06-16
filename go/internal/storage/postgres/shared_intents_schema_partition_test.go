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
		"shared_projection_intents_domain_partition_pending_idx",
		"shared_projection_intents_domain_unhashed_pending_idx",
	} {
		if !strings.Contains(sqlStr, want) {
			t.Fatalf("schema SQL missing %q", want)
		}
	}
}
