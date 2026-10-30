// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9661 disposable PostgreSQL proof that the workload-to-ECS-service binding declaration census reads active-generation Terraform and Terragrunt content entities with verbatim metadata, excludes tombstones and superseded generations and other entity types and facts, reports truncation instead of silently subsetting, and plans index-backed with no sequential scan on fact_records at 25k-row scale; isolated temp tables per test, rolled back
//eshu:readiness:env ESHU_POSTGRES_TEST_DSN

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// txQueryer adapts *sql.Tx to db.Queryer for the isolated fixture.
type txQueryer struct{ tx *sql.Tx }

func (q txQueryer) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return q.tx.QueryContext(ctx, query, args...)
}

// openECSBindingFixtureTx opens an isolated fixture whose search_path
// resolves scope_generations and fact_records to temp tables holding only
// the columns the census reads. Cleanup rolls the fixture back.
func openECSBindingFixtureTx(t *testing.T) (context.Context, *sql.Tx) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the ECS binding census live proof")
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	t.Cleanup(cancel)
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin isolated fixture: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	for _, stmt := range []string{
		`CREATE TEMP TABLE scope_generations (
		    scope_id text NOT NULL,
		    generation_id text NOT NULL,
		    status text NOT NULL
		)`,
		`CREATE TEMP TABLE fact_records (
		    scope_id text NOT NULL,
		    generation_id text NOT NULL,
		    fact_kind text NOT NULL,
		    is_tombstone boolean NOT NULL,
		    payload jsonb NOT NULL
		)`,
		`CREATE INDEX ecs_binding_fixture_gen_idx ON scope_generations (scope_id, generation_id)`,
		`CREATE INDEX ecs_binding_fixture_fact_idx ON fact_records (scope_id, generation_id)`,
		`CREATE INDEX ecs_binding_fixture_iac_idx ON fact_records
		    (scope_id, generation_id, (payload->>'entity_type'), (payload->>'entity_name'), (payload->>'entity_id'))
		    WHERE fact_kind = 'content_entity'
		      AND is_tombstone = FALSE
		      AND payload->>'entity_type' IN (
		          'TerraformResource',
		          'TerraformModule',
		          'TerraformVariable',
		          'TerraformLocal',
		          'TerragruntConfig',
		          'TerragruntInput'
		      )`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("create fixture table: %v", err)
		}
	}
	return ctx, tx
}

func seedECSBindingFixture(t *testing.T, ctx context.Context, tx *sql.Tx) {
	t.Helper()
	for _, gen := range [][3]string{
		{"repo-scope", "gen-active", "active"},
		{"repo-scope", "gen-old", "superseded"},
		{"aws-scope", "gen-aws", "active"},
	} {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO scope_generations (scope_id, generation_id, status) VALUES ($1, $2, $3)`,
			gen[0], gen[1], gen[2]); err != nil {
			t.Fatalf("seed generation: %v", err)
		}
	}
	seed := []struct {
		scope, gen, kind, payload string
		tombstone                 bool
	}{
		{"repo-scope", "gen-active", "content_entity", `{"entity_type":"TerraformResource","entity_name":"aws_ecs_service.api","entity_id":"e1","repo_id":"repo-app","relative_path":"main.tf","entity_metadata":{"resource_type":"aws_ecs_service","resource_name":"api","attributes":{"name":"api","cluster":"arn:aws:ecs:us-east-1:111122223333:cluster/prod"}}}`, false},
		{"repo-scope", "gen-active", "content_entity", `{"entity_type":"TerraformVariable","entity_name":"service_name","entity_id":"e2","repo_id":"repo-app","relative_path":"variables.tf","entity_metadata":{"name":"service_name","default":"\"api\""}}`, false},
		{"repo-scope", "gen-active", "content_entity", `{"entity_type":"TerraformInput","entity_name":"other","entity_id":"e3","repo_id":"repo-app","relative_path":"x.tf","entity_metadata":{}}`, false},
		{"repo-scope", "gen-active", "content_entity", `{"entity_type":"TerraformResource","entity_name":"aws_ecs_service.old","entity_id":"e4","repo_id":"repo-app","relative_path":"old.tf","entity_metadata":{"resource_type":"aws_ecs_service"}}`, true},
		{"repo-scope", "gen-old", "content_entity", `{"entity_type":"TerraformResource","entity_name":"aws_ecs_service.stale","entity_id":"e5","repo_id":"repo-app","relative_path":"stale.tf","entity_metadata":{"resource_type":"aws_ecs_service"}}`, false},
		{"aws-scope", "gen-aws", "aws_resource", `{"resource_type":"aws_ecs_service"}`, false},
	}
	for i, row := range seed {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO fact_records (scope_id, generation_id, fact_kind, is_tombstone, payload) VALUES ($1, $2, $3, $4, $5::jsonb)`,
			row.scope, row.gen, row.kind, row.tombstone, row.payload); err != nil {
			t.Fatalf("seed fact %d: %v", i, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `ANALYZE scope_generations; ANALYZE fact_records`); err != nil {
		t.Fatalf("analyze fixture: %v", err)
	}
}

// TestECSBindingCensusReadsActiveDeclarationsLive proves the shipped
// census returns active declarations with verbatim metadata while
// excluding tombstones, superseded generations, other entity types,
// and non-content facts.
func TestECSBindingCensusReadsActiveDeclarationsLive(t *testing.T) {
	ctx, tx := openECSBindingFixtureTx(t)
	seedECSBindingFixture(t, ctx, tx)

	result, err := NewECSBindingDeclarationCensusStore(txQueryer{tx}, 100).ListECSBindingDeclarations(ctx)
	if err != nil {
		t.Fatalf("ListECSBindingDeclarations: %v", err)
	}
	if result.Truncated {
		t.Fatalf("census truncated unexpectedly")
	}
	if len(result.Declarations) != 2 {
		t.Fatalf("declarations = %d, want 2 (resource + variable)", len(result.Declarations))
	}
	resource := result.Declarations[0]
	if resource.RepoID != "repo-app" || resource.Path != "main.tf" || resource.EntityType != "TerraformResource" {
		t.Fatalf("resource declaration = %#v", resource)
	}
	attrs, _ := resource.Metadata["attributes"].(map[string]any)
	if attrs["name"] != "api" || attrs["cluster"] != "arn:aws:ecs:us-east-1:111122223333:cluster/prod" {
		t.Fatalf("resource attributes = %#v", attrs)
	}
	variable := result.Declarations[1]
	if variable.EntityType != "TerraformVariable" || variable.Metadata["default"] != `"api"` {
		t.Fatalf("variable declaration = %#v", variable)
	}
}

// TestECSBindingCensusTruncationIsVisibleLive proves the row cap reports
// instead of silently subsetting the resolution context.
func TestECSBindingCensusTruncationIsVisibleLive(t *testing.T) {
	ctx, tx := openECSBindingFixtureTx(t)
	seedECSBindingFixture(t, ctx, tx)

	result, err := NewECSBindingDeclarationCensusStore(txQueryer{tx}, 1).ListECSBindingDeclarations(ctx)
	if err != nil {
		t.Fatalf("ListECSBindingDeclarations: %v", err)
	}
	if !result.Truncated || len(result.Declarations) != 1 {
		t.Fatalf("truncated=%v rows=%d, want true/1", result.Truncated, len(result.Declarations))
	}
}

// TestECSBindingCensusPlanIsIndexBackedLive runs the shipped census SQL
// (derived from the constant, read-only EXPLAIN tail) against prod-shape
// indexes over a 25k-row fact table and requires the fact leg off
// sequential scans: the materialization must not corpus-scan
// fact_records at QA scale.
func TestECSBindingCensusPlanIsIndexBackedLive(t *testing.T) {
	ctx, tx := openECSBindingFixtureTx(t)
	seedECSBindingFixture(t, ctx, tx)
	// Volume distractors: other entity types, other scopes, and other
	// fact kinds, so a sequential scan would be visibly wrong.
	for _, stmt := range []string{
		`INSERT INTO fact_records (scope_id, generation_id, fact_kind, is_tombstone, payload)
		 SELECT 'repo-scope', 'gen-active', 'content_entity', FALSE,
		        jsonb_build_object('entity_type', 'TerraformOutput', 'entity_name', 'o' || g, 'entity_id', 'd' || g, 'repo_id', 'repo-app', 'relative_path', 'o.tf', 'entity_metadata', '{}')
		 FROM generate_series(1, 15000) AS g`,
		`INSERT INTO fact_records (scope_id, generation_id, fact_kind, is_tombstone, payload)
		 SELECT 'other-scope', 'gen-other', 'content_entity', FALSE,
		        jsonb_build_object('entity_type', 'TerraformResource', 'entity_name', 'aws_s3_bucket.b' || g, 'entity_id', 'x' || g, 'repo_id', 'repo-other', 'relative_path', 'b.tf', 'entity_metadata', '{}')
		 FROM generate_series(1, 5000) AS g`,
		`INSERT INTO fact_records (scope_id, generation_id, fact_kind, is_tombstone, payload)
		 SELECT 'aws-scope', 'gen-aws', 'aws_resource', FALSE,
		        jsonb_build_object('resource_type', 'aws_ecs_service', 'resource_id', 'r' || g)
		 FROM generate_series(1, 5000) AS g`,
		`INSERT INTO scope_generations (scope_id, generation_id, status) VALUES ('other-scope', 'gen-other', 'active')`,
		`ANALYZE scope_generations; ANALYZE fact_records`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed plan volume: %v", err)
		}
	}

	rows, err := tx.QueryContext(ctx, "EXPLAIN "+ecsBindingCensusSQL, 100)
	if err != nil {
		t.Fatalf("explain census: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan.WriteString(line + "\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate plan: %v", err)
	}
	text := plan.String()
	if strings.Contains(text, "Seq Scan on fact_records") {
		t.Fatalf("census plan scans fact_records sequentially:\n%s", text)
	}
	if !strings.Contains(text, "Index") {
		t.Fatalf("census plan uses no index:\n%s", text)
	}
}

// TestECSBindingCensusEntityTypesMatchSQL pins the shipped IN-list to the
// census entity-type set: adding a resolver context family without
// extending the census read (or vice versa) fails here, not in prod.
func TestECSBindingCensusEntityTypesMatchSQL(t *testing.T) {
	for _, entityType := range ecsBindingCensusEntityTypes {
		if !strings.Contains(ecsBindingCensusSQL, "'"+entityType+"'") {
			t.Errorf("census SQL lacks entity type %q", entityType)
		}
	}
	_ = reducer.ECSBindingCensusCap()
}
