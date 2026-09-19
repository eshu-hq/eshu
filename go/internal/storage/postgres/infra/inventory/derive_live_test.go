// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// Live proofs for the infra_resource_entities derive step (#6793). Run with:
//
//	ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	  go test ./internal/storage/postgres/infra/inventory -run Live -count=1 -race -v

func liveDB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the real-Postgres infra inventory proofs")
	}
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	schemaCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := postgres.ApplyBootstrap(schemaCtx, postgres.SQLDB{DB: sqlDB}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}
	ctx, cancelTest := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancelTest)
	return sqlDB, ctx
}

var repoCounter atomic.Int64

// uniqueRepo returns a repo id no other test touches, so live tests can run in
// one shared database without cleaning up after each other.
func uniqueRepo(t *testing.T) string {
	return fmt.Sprintf("repo-6793-%s-%d-%d", strings.ToLower(t.Name()), time.Now().UnixNano(), repoCounter.Add(1))
}

type contentRow struct {
	id, path, entityType, name string
	metadata                   string
}

func putContent(t *testing.T, ctx context.Context, sqlDB *sql.DB, repo string, rows ...contentRow) {
	t.Helper()
	for _, row := range rows {
		meta := row.metadata
		if meta == "" {
			meta = "{}"
		}
		if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, source_cache, metadata, indexed_at)
VALUES ($1, $2, $3, $4, $5, 1, 2, '', $6::jsonb, now())
ON CONFLICT (entity_id) DO UPDATE SET relative_path = EXCLUDED.relative_path,
    entity_type = EXCLUDED.entity_type, entity_name = EXCLUDED.entity_name, metadata = EXCLUDED.metadata`,
			repo+"/"+row.id, repo, row.path, row.entityType, row.name, meta); err != nil {
			t.Fatalf("seed content row %s: %v", row.id, err)
		}
	}
}

func deleteContent(t *testing.T, ctx context.Context, sqlDB *sql.DB, repo string, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if _, err := sqlDB.ExecContext(ctx, `DELETE FROM content_entities WHERE entity_id = $1`, repo+"/"+id); err != nil {
			t.Fatalf("delete content row %s: %v", id, err)
		}
	}
}

// tableRows returns "id|path|label|name|kind|resource_type|data_type|provider|
// environment|resource_service|resource_category|service_kind|scope|generation"
// for every table row of repo, ordered by id.
func tableRows(t *testing.T, ctx context.Context, sqlDB *sql.DB, repo string) []string {
	t.Helper()
	rows, err := sqlDB.QueryContext(ctx, `
SELECT substr(entity_id, length($1) + 2), relative_path, label, entity_name, kind, resource_type,
       data_type, provider, environment, resource_service, resource_category, service_kind,
       scope_id, generation_id
FROM infra_resource_entities WHERE repo_id = $1 ORDER BY entity_id`, repo)
	if err != nil {
		t.Fatalf("read table: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		cols := make([]string, 14)
		ptrs := make([]any, len(cols))
		for i := range cols {
			ptrs[i] = &cols[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, strings.Join(cols, "|"))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

func TestMirrorPathsLiveDerivesOnlyEntityDerivedLabelsWithCanonicalValues(t *testing.T) {
	sqlDB, ctx := liveDB(t)
	repo := uniqueRepo(t)
	putContent(t, ctx, sqlDB, repo,
		contentRow{
			"tf1", "main.tf", "TerraformResource", "aws_s3_bucket.a",
			`{"provider":" aws ","resource_type":"aws_s3_bucket","resource_service":"s3","resource_category":"storage","environment":"  "}`,
		},
		contentRow{"ds1", "main.tf", "TerraformDataSource", "data.x", `{"data_type":"aws_iam_policy_document","provider":"aws"}`},
		contentRow{"k1", "k8s/app.yaml", "K8sResource", "app", `{"kind":"Deployment","environment":"prod"}`},
		// Not mirrored: a code entity, and a mixed-writer label kept graph-only.
		contentRow{"fn1", "main.go", "Function", "main", `{}`},
		contentRow{"mod1", "main.tf", "TerraformModule", "vpc", `{"provider":"aws"}`},
	)

	target := inventory.Target{RepoID: repo, ScopeID: "scope-a", GenerationID: "gen-1"}
	stats, err := inventory.MirrorPaths(ctx, postgres.SQLDB{DB: sqlDB}, target, []string{"main.tf", "k8s/app.yaml", "main.go"})
	if err != nil {
		t.Fatalf("MirrorPaths() error = %v", err)
	}
	if stats.Inserted != 3 || stats.Deleted != 0 {
		t.Fatalf("stats = %+v, want 3 inserted, 0 deleted", stats)
	}
	want := []string{
		"ds1|main.tf|TerraformDataSource|data.x|||aws_iam_policy_document|aws|||||scope-a|gen-1",
		"k1|k8s/app.yaml|K8sResource|app|Deployment||||prod||||scope-a|gen-1",
		"tf1|main.tf|TerraformResource|aws_s3_bucket.a||aws_s3_bucket||aws||s3|storage||scope-a|gen-1",
	}
	if got := tableRows(t, ctx, sqlDB, repo); !reflect.DeepEqual(got, want) {
		t.Fatalf("table rows:\n got %q\nwant %q", got, want)
	}

	// Duplicate delivery: replaying the same Write converges on identical rows.
	if _, err := inventory.MirrorPaths(ctx, postgres.SQLDB{DB: sqlDB}, target, []string{"main.tf", "k8s/app.yaml", "main.go"}); err != nil {
		t.Fatalf("replay MirrorPaths() error = %v", err)
	}
	if got := tableRows(t, ctx, sqlDB, repo); !reflect.DeepEqual(got, want) {
		t.Fatalf("replayed table rows:\n got %q\nwant %q", got, want)
	}
}

func TestMirrorPathsLiveDeltaTouchesOnlyItsPathsAndTombstonesConverge(t *testing.T) {
	sqlDB, ctx := liveDB(t)
	repo := uniqueRepo(t)
	database := postgres.SQLDB{DB: sqlDB}
	putContent(t, ctx, sqlDB, repo,
		contentRow{"a1", "a.tf", "TerraformResource", "r.a1", `{"provider":"aws"}`},
		contentRow{"a2", "a.tf", "TerraformResource", "r.a2", `{"provider":"aws"}`},
		contentRow{"b1", "b.tf", "TerraformResource", "r.b1", `{"provider":"google"}`},
		contentRow{"c1", "c.tf", "TerraformVariable", "v.c1", `{}`},
	)
	if _, err := inventory.MirrorPaths(ctx, database, inventory.Target{RepoID: repo, GenerationID: "gen-full"},
		[]string{"a.tf", "b.tf", "c.tf"}); err != nil {
		t.Fatalf("full MirrorPaths() error = %v", err)
	}

	// Delta generation touches only a.tf: a2 removed, a1 changed provider.
	// b.tf keeps its row and its older generation id.
	deleteContent(t, ctx, sqlDB, repo, "a2")
	putContent(t, ctx, sqlDB, repo, contentRow{"a1", "a.tf", "TerraformResource", "r.a1", `{"provider":"azurerm"}`})
	// c.tf is tombstoned: the content writer deleted every entity on it.
	deleteContent(t, ctx, sqlDB, repo, "c1")
	if _, err := inventory.MirrorPaths(ctx, database, inventory.Target{RepoID: repo, GenerationID: "gen-delta"},
		[]string{"a.tf", "c.tf"}); err != nil {
		t.Fatalf("delta MirrorPaths() error = %v", err)
	}
	want := []string{
		"a1|a.tf|TerraformResource|r.a1||||azurerm||||||gen-delta",
		"b1|b.tf|TerraformResource|r.b1||||google||||||gen-full",
	}
	if got := tableRows(t, ctx, sqlDB, repo); !reflect.DeepEqual(got, want) {
		t.Fatalf("after delta:\n got %q\nwant %q", got, want)
	}

	// An entity whose content row moved to a path outside the derived chunk
	// converges instead of failing the derive on the primary key.
	putContent(t, ctx, sqlDB, repo, contentRow{"b1", "moved/b.tf", "TerraformResource", "r.b1", `{"provider":"google"}`})
	if _, err := inventory.MirrorPaths(ctx, database, inventory.Target{RepoID: repo, GenerationID: "gen-move"},
		[]string{"moved/b.tf"}); err != nil {
		t.Fatalf("move MirrorPaths() error = %v", err)
	}
	want = []string{
		"a1|a.tf|TerraformResource|r.a1||||azurerm||||||gen-delta",
		"b1|moved/b.tf|TerraformResource|r.b1||||google||||||gen-move",
	}
	if got := tableRows(t, ctx, sqlDB, repo); !reflect.DeepEqual(got, want) {
		t.Fatalf("after move:\n got %q\nwant %q", got, want)
	}
}

func TestMirrorRepoLiveRederivesWholeRepoAndDropsStaleRows(t *testing.T) {
	sqlDB, ctx := liveDB(t)
	repo := uniqueRepo(t)
	database := postgres.SQLDB{DB: sqlDB}
	putContent(t, ctx, sqlDB, repo,
		contentRow{"x1", "x.tf", "TerraformResource", "r.x1", `{"provider":"aws"}`},
		contentRow{"y1", "y/app.yaml", "K8sResource", "y", `{"kind":"Service"}`},
	)
	if _, err := inventory.MirrorPaths(ctx, database, inventory.Target{RepoID: repo, GenerationID: "g1"},
		[]string{"x.tf", "y/app.yaml"}); err != nil {
		t.Fatalf("MirrorPaths() error = %v", err)
	}
	// Content drifts without a derive (for example written before the derive
	// step existed); the backfill must converge the whole repo.
	deleteContent(t, ctx, sqlDB, repo, "x1")
	putContent(t, ctx, sqlDB, repo, contentRow{"z1", "z.tf", "TerraformLocal", "local.z", `{}`})

	stats, err := inventory.MirrorRepo(ctx, database, repo)
	if err != nil {
		t.Fatalf("MirrorRepo() error = %v", err)
	}
	if stats.Deleted != 2 || stats.Inserted != 2 {
		t.Fatalf("stats = %+v, want 2 deleted, 2 inserted", stats)
	}
	want := []string{
		"y1|y/app.yaml|K8sResource|y|Service|||||||||",
		"z1|z.tf|TerraformLocal|local.z||||||||||",
	}
	if got := tableRows(t, ctx, sqlDB, repo); !reflect.DeepEqual(got, want) {
		t.Fatalf("after backfill:\n got %q\nwant %q", got, want)
	}
}
