// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// TestMirrorPathsLiveLargeRepositoryCost is the projector no-regression probe
// for the derive step (#6793). One large repository has 20,000 infra entities
// over 3,500 files plus 30,000 non-infra entities over 5,000 files, so a full
// generation touches 8,500 paths. It logs the full-generation and 50-path delta
// derive wall time so the cost can be compared against the content writer's
// existing stages. It asserts only correctness (row counts), not a latency
// threshold, because host load varies.
//
//	ESHU_POSTGRES_DSN=... go test ./internal/storage/postgres/infra/inventory \
//	  -run TestMirrorPathsLiveLargeRepositoryCost -count=1 -v
func TestMirrorPathsLiveLargeRepositoryCost(t *testing.T) {
	sqlDB, ctx := liveDB(t)
	database := postgres.SQLDB{DB: sqlDB}
	repo := uniqueRepo(t)

	seedStart := time.Now()
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, source_cache, metadata, indexed_at)
SELECT $1 || '/tf' || g, $1, 'infra/f' || (g % 3500) || '.tf',
       (ARRAY['TerraformResource','TerraformVariable','TerraformLocal','TerraformDataSource'])[1 + g % 4],
       'r' || g, 1, 20, repeat('x', 400),
       jsonb_build_object('provider', 'aws', 'resource_type', 'aws_t' || (g % 40), 'resource_service', 's' || (g % 9)),
       now()
FROM generate_series(1, 20000) g
UNION ALL
SELECT $1 || '/code' || g, $1, 'src/m' || (g % 5000) || '.go', 'Function', 'f' || g, 1, 30, repeat('y', 800),
       '{}'::jsonb, now()
FROM generate_series(1, 30000) g`, repo); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Logf("seeded 50000 content rows in %s", time.Since(seedStart))

	paths := make([]string, 0, 8500)
	for i := 0; i < 3500; i++ {
		paths = append(paths, fmt.Sprintf("infra/f%d.tf", i))
	}
	for i := 0; i < 5000; i++ {
		paths = append(paths, fmt.Sprintf("src/m%d.go", i))
	}
	target := inventory.Target{RepoID: repo, ScopeID: "scope", GenerationID: "gen-full"}

	start := time.Now()
	stats, err := inventory.MirrorPaths(ctx, database, target, paths)
	if err != nil {
		t.Fatalf("full MirrorPaths() error = %v", err)
	}
	full := time.Since(start)
	if stats.Inserted != 20000 {
		t.Fatalf("full derive inserted %d, want 20000", stats.Inserted)
	}

	start = time.Now()
	stats, err = inventory.MirrorPaths(ctx, database, target, paths)
	if err != nil {
		t.Fatalf("replay MirrorPaths() error = %v", err)
	}
	replay := time.Since(start)
	if stats.Deleted != 20000 || stats.Inserted != 20000 {
		t.Fatalf("replay stats = %+v, want 20000 deleted and inserted", stats)
	}

	start = time.Now()
	if _, err := inventory.MirrorPaths(ctx, database, target, paths[:50]); err != nil {
		t.Fatalf("delta MirrorPaths() error = %v", err)
	}
	delta := time.Since(start)
	t.Logf("Performance Evidence: derive 8500 paths (20000 infra rows) first=%s replay=%s; 50-path delta=%s",
		full, replay, delta)
}
