// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"maps"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// TestGenerationRetentionRowCountsAttributeSharedRowsOnceLive proves the row
// count charges each doomed content row to exactly one candidate: the newest
// candidate, in the order passed, whose facts name its key. Per table the
// per-generation counts then sum to the rows the prunes delete (#6809).
//
// Seed, one scope with an active generation and three superseded candidates:
// key "shared" has facts in gen-1 and gen-3, key "only-2" only in gen-2, and
// key "protected" in gen-1 and the active generation. Each key has one
// content_entities row, one content_files row, two content_file_references
// rows and one infra_resource_entities mirror row.
func TestGenerationRetentionRowCountsAttributeSharedRowsOnceLive(t *testing.T) {
	database, ctx := openGenerationRetentionMigratedSchema(t)
	execRetentionSeed(t, ctx, database, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload)
VALUES ('scope-attr', 'repository', 'git', 'attr', 'git', 'attr', now(), now(), 'active', '{}'::jsonb)`)
	for _, gen := range []struct{ id, status string }{
		{"gen-attr-active", "active"}, {"gen-1", "superseded"}, {"gen-2", "superseded"}, {"gen-3", "superseded"},
	} {
		execRetentionSeed(t, ctx, database, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, 'scope-attr', 'snapshot', now(), now(), $2)`, gen.id, gen.status)
	}
	for key, generations := range map[string][]string{
		"shared":    {"gen-1", "gen-3"},
		"only-2":    {"gen-2"},
		"protected": {"gen-1", "gen-attr-active"},
	} {
		for _, generation := range generations {
			seedGenerationRetentionPruneFact(t, ctx, database, generation, key, "repo-1", false)
		}
		seedGenerationRetentionPruneRows(t, ctx, database, "repo-1", key)
		execRetentionSeed(t, ctx, database, `
INSERT INTO infra_resource_entities (entity_id, repo_id, relative_path, label, entity_name, updated_at)
VALUES ($1, 'repo-1', $1, 'TerraformResource', 'n', now())`, key)
	}

	content := func(n int64) map[string]int64 {
		return map[string]int64{
			"content_entities": n, "content_files": n, "content_file_references": 2 * n, "infra_resource_entities": n,
		}
	}
	for _, tc := range []struct {
		name  string
		order []string
		want  map[string]map[string]int64
	}{
		// Oldest first, as the store passes them: gen-3 is newest and holds "shared".
		{
			"store-order",
			[]string{"gen-1", "gen-2", "gen-3"},
			map[string]map[string]int64{"gen-1": content(0), "gen-2": content(1), "gen-3": content(1)},
		},
		// The rank comes from the order passed, not from the generation id.
		{
			"reversed",
			[]string{"gen-3", "gen-2", "gen-1"},
			map[string]map[string]int64{"gen-1": content(1), "gen-2": content(1), "gen-3": content(0)},
		},
	} {
		tx, err := SQLDB{DB: database}.Begin(ctx)
		if err != nil {
			t.Fatalf("%s: begin: %v", tc.name, err)
		}
		totals, perGeneration, _, err := GenerationRetentionStore{}.countRows(ctx, tx, tc.order)
		if err != nil {
			_ = tx.Rollback()
			t.Fatalf("%s: countRows() error = %v", tc.name, err)
		}
		for generation, want := range tc.want {
			got := make(map[string]int64, len(want))
			for table := range want {
				got[table] = perGeneration[generation][table]
			}
			if !maps.Equal(got, want) {
				t.Errorf("%s: %s content counts = %v, want %v", tc.name, generation, got, want)
			}
		}
		// The sums equal what the prunes delete in this transaction.
		deleted := map[string]int64{}
		for table, statement := range map[string]string{
			"content_file_references": pruneContentFileReferencesForGenerationsQuery,
			"content_entities":        pruneContentEntitiesForGenerationsQuery,
			"content_files":           pruneContentFilesForGenerationsQuery,
		} {
			if deleted[table], err = execRowsAffected(ctx, tx, statement, tc.order); err != nil {
				_ = tx.Rollback()
				t.Fatalf("%s: prune %s: %v", tc.name, table, err)
			}
		}
		if deleted["infra_resource_entities"], err = inventory.DeleteOrphanedRows(ctx, tx, tc.order); err != nil {
			_ = tx.Rollback()
			t.Fatalf("%s: infra orphan delete: %v", tc.name, err)
		}
		_ = tx.Rollback()
		for table, n := range deleted {
			if totals[table] != n {
				t.Errorf("%s: %s counts sum to %d, want the %d rows deleted", tc.name, table, totals[table], n)
			}
		}
	}
}
