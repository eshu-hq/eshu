// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestSharedIntentAcceptanceCompletedIndexMigration pins the #7929
// reachability-selection fix: the per-run max(completed_at) LATERAL probe in
// the pending-code-reachability loader must be servable from the covering
// index as an index-only scan instead of a bitmap heap walk over every
// completed intent of the run (measured AFTER plan still visits one index
// entry per completed intent; max() over the two-value IN list does not
// short-circuit to a backward limit scan). The probe filters on the four equality
// columns (scope_id, acceptance_unit_id, source_run_id, generation_id), one
// of two projection_domain values, and completed_at IS NOT NULL, then takes
// max(completed_at); the migration-008 acceptance_lookup_idx lacks
// generation_id and completed_at, so the probe re-reads the whole intent
// heap (see docs/internal/evidence/7929-polling-elimination.md once written).
func TestSharedIntentAcceptanceCompletedIndexMigration(t *testing.T) {
	t.Parallel()

	const indexName = "shared_projection_intents_acceptance_completed_idx"
	var found string
	for _, def := range BootstrapDefinitions() {
		if strings.Contains(def.SQL, indexName) {
			found = def.SQL
			break
		}
	}
	if found == "" {
		t.Fatalf("no bootstrap migration creates %s", indexName)
	}
	for _, want := range []string{
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS " + indexName,
		"ON shared_projection_intents (scope_id, acceptance_unit_id, source_run_id, generation_id, projection_domain, completed_at)",
		"WHERE completed_at IS NOT NULL",
	} {
		if !strings.Contains(found, want) {
			t.Errorf("migration SQL missing %q:\n%s", want, found)
		}
	}
	// The migration coordinator runs a lone concurrent index statement
	// outside a transaction; a second statement in the file would run
	// inside one and fail the build.
	if n := strings.Count(found, ";"); n != 1 {
		t.Errorf("migration holds %d semicolons, want exactly 1 (lone statement, no semicolons in comments)", n)
	}
	if strings.Contains(strings.ToUpper(found), "DROP INDEX") {
		t.Errorf("migration must add under a new name and keep the existing lookup index; dropping it needs its own owner review:\n%s", found)
	}
}
