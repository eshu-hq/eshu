// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestGenerationPruneGenerationIndexMigrations pins the #7419 indexes: the
// per-prune probes filter on generation_id alone, and none of
// fact_replay_events, graph_projection_phase_state, or
// graph_projection_phase_repair_queue carries a generation_id-leading index
// (the replay table's index leads with work_item_id; the phase tables bury
// generation_id fourth in their primary keys), so every prune scanned each
// table whole. Each build must stay a sole CONCURRENTLY statement: the
// migration coordinator runs such a file in autocommit without the bootstrap
// lock_timeout (#7004), and PostgreSQL rejects a concurrent build inside a
// transaction block.
func TestGenerationPruneGenerationIndexMigrations(t *testing.T) {
	t.Parallel()

	cases := []struct {
		migration string
		table     string
	}{
		{"fact_replay_events_generation_idx", "fact_replay_events"},
		{"graph_projection_phase_state_generation_idx", "graph_projection_phase_state"},
		{"graph_projection_phase_repair_queue_generation_idx", "graph_projection_phase_repair_queue"},
	}
	for _, tc := range cases {
		t.Run(tc.migration, func(t *testing.T) {
			t.Parallel()

			migration := MigrationSQL(tc.migration)
			if migration == "" {
				t.Fatalf("generation prune generation index migration %q missing", tc.migration)
			}
			normalized := strings.Join(strings.Fields(migration), " ")
			for _, fragment := range []string{
				"CREATE INDEX CONCURRENTLY IF NOT EXISTS " + tc.migration,
				"ON " + tc.table + " (generation_id)",
			} {
				if !strings.Contains(normalized, strings.Join(strings.Fields(fragment), " ")) {
					t.Fatalf("generation prune migration %q missing %q", tc.migration, fragment)
				}
			}
			_, statement, found := strings.Cut(migration, "CREATE INDEX")
			if !found {
				t.Fatalf("generation prune migration %q has no CREATE INDEX statement", tc.migration)
			}
			if terminators := strings.Count(statement, ";"); terminators != 1 {
				t.Fatalf("concurrent index migration %q has %d SQL statements, want one", tc.migration, terminators)
			}
		})
	}
}
