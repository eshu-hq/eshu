// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const (
	ecsIndexProofDSN        = "ESHU_TEST_ECS_RUNTIME_INDEX_POSTGRES_DSN"
	ecsIndexProofDisposable = "ESHU_TEST_ECS_RUNTIME_INDEX_POSTGRES_DISPOSABLE"
)

var ecsRuntimeIndexNames = []string{
	"fact_records_ecs_service_arn_idx",
	"fact_records_ecs_task_scope_cluster_group_idx",
}

// TestECSRuntimeIndexFreshBootstrapLive proves the complete ordered migration
// tree installs both indexes on a new database.
func TestECSRuntimeIndexFreshBootstrapLive(t *testing.T) {
	ctx, database := postgresproof.OpenDisposableDatabase(t,
		os.Getenv(ecsIndexProofDSN), os.Getenv(ecsIndexProofDisposable), 5*time.Minute)
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("fresh ApplyBootstrap: %v", err)
	}
	for _, name := range ecsRuntimeIndexNames {
		assertECSIndexState(t, ctx, database, name, true)
	}
}

// TestECSRuntimeIndexUpgradeLive proves the shipped migration runner creates
// both indexes over populated facts while an ordinary fact writer progresses,
// then recovers a failed concurrent build on restart.
func TestECSRuntimeIndexUpgradeLive(t *testing.T) {
	ctx, database := postgresproof.OpenDisposableDatabase(t,
		os.Getenv(ecsIndexProofDSN), os.Getenv(ecsIndexProofDisposable), 5*time.Minute)
	definitions := BootstrapDefinitions()
	boundary := -1
	for i, definition := range definitions {
		if definition.Name == "ecs_runtime_service_arn_index" {
			boundary = i
			break
		}
	}
	if boundary < 0 || boundary+1 >= len(definitions) || definitions[boundary+1].Name != "ecs_runtime_task_scope_index" {
		t.Fatalf("ECS index migration order is missing or nonadjacent: boundary=%d", boundary)
	}
	exec := SQLDB{DB: database}
	if err := applyBootstrapDefinitionsWith(ctx, exec, definitions[:boundary], slog.Default(), schemaBootstrapCoordination{}); err != nil {
		t.Fatalf("apply prior schema: %v", err)
	}
	seedECSRuntimeIndexFacts(t, ctx, database)
	for _, name := range ecsRuntimeIndexNames {
		assertECSIndexState(t, ctx, database, name, false)
	}

	writerCtx, stopWriter := context.WithCancel(ctx)
	ready := make(chan error, 1)
	done := make(chan ecsIndexWriterProgress, 1)
	go func() { done <- runECSIndexWriter(writerCtx, database, ready) }()
	if err := <-ready; err != nil {
		stopWriter()
		<-done
		t.Fatalf("start fact writer: %v", err)
	}
	started := time.Now()
	upgradeErr := applyBootstrapDefinitionsWith(ctx, exec, definitions[boundary:boundary+2], slog.Default(), schemaBootstrapCoordination{})
	upgradeDuration := time.Since(started)
	stopWriter()
	writer := <-done
	if upgradeErr != nil {
		t.Fatalf("upgrade populated schema: %v", upgradeErr)
	}
	if writer.err != nil || writer.cycles < 2 {
		t.Fatalf("writer progress during upgrade: cycles=%d err=%v", writer.cycles, writer.err)
	}
	t.Logf("populated upgrade=%s writer cycles=%d max cycle=%s", upgradeDuration, writer.cycles, writer.maxCycle)
	for _, name := range ecsRuntimeIndexNames {
		assertECSIndexState(t, ctx, database, name, true)
	}
	if err := ApplyBootstrap(ctx, exec); err != nil {
		t.Fatalf("restart bootstrap: %v", err)
	}

	name := ecsRuntimeIndexNames[0]
	if _, err := database.ExecContext(ctx, "DROP INDEX CONCURRENTLY "+name); err != nil {
		t.Fatalf("drop index before interrupted-build proof: %v", err)
	}
	invalidDDL := "CREATE UNIQUE INDEX CONCURRENTLY " + name +
		" ON fact_records (fact_kind) WHERE fact_kind = 'aws_resource' AND is_tombstone = FALSE"
	if _, err := database.ExecContext(ctx, invalidDDL); err == nil {
		t.Fatal("duplicate facts unexpectedly allowed unique index")
	}
	assertECSIndexValidity(t, ctx, database, name, false)
	if err := ApplyBootstrap(ctx, exec); err != nil {
		t.Fatalf("recover invalid index through actual bootstrap: %v", err)
	}
	for _, indexName := range ecsRuntimeIndexNames {
		assertECSIndexState(t, ctx, database, indexName, true)
	}
}

type ecsIndexWriterProgress struct {
	cycles   int
	maxCycle time.Duration
	err      error
}

func runECSIndexWriter(ctx context.Context, database *sql.DB, ready chan<- error) ecsIndexWriterProgress {
	var progress ecsIndexWriterProgress
	for n := 0; ctx.Err() == nil; n++ {
		started := time.Now()
		id := fmt.Sprintf("ecs-index-writer:%d", n)
		_, err := database.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind,
  stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, 'scope:ecs-index-proof', 'generation:ecs-index-proof', 'aws_resource',
  $1, 'proof', $1, clock_timestamp(), clock_timestamp(),
  '{"resource_type":"aws_ecs_task","state":"RUNNING","attributes":{"cluster_arn":"arn:aws:ecs:us-east-1:123456789012:cluster/proof","group":"service:proof"}}'::jsonb)`, id)
		if err == nil {
			_, err = database.ExecContext(ctx, "DELETE FROM fact_records WHERE fact_id = $1", id)
		}
		if err != nil {
			if ctx.Err() == nil {
				progress.err = err
			}
			if n == 0 {
				ready <- progress.err
			}
			return progress
		}
		progress.cycles++
		if elapsed := time.Since(started); elapsed > progress.maxCycle {
			progress.maxCycle = elapsed
		}
		if n == 0 {
			ready <- nil
		}
	}
	return progress
}

func seedECSRuntimeIndexFacts(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	_, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key,
  collector_kind, partition_key, observed_at, ingested_at, status)
VALUES ('scope:ecs-index-proof', 'aws_account', 'proof', 'proof', 'proof',
  'proof', clock_timestamp(), clock_timestamp(), 'active');
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at,
  ingested_at, status, activated_at)
VALUES ('generation:ecs-index-proof', 'scope:ecs-index-proof', 'proof',
  clock_timestamp(), clock_timestamp(), 'active', clock_timestamp());
UPDATE ingestion_scopes SET active_generation_id = 'generation:ecs-index-proof'
WHERE scope_id = 'scope:ecs-index-proof';
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind,
  stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'ecs-index:' || n, 'scope:ecs-index-proof', 'generation:ecs-index-proof',
  'aws_resource', 'ecs-index:' || n, 'proof', 'ecs-index:' || n,
  clock_timestamp(), clock_timestamp(),
  CASE WHEN n % 2 = 0 THEN
    jsonb_build_object('resource_type', 'aws_ecs_service', 'arn',
      'arn:aws:ecs:us-east-1:123456789012:service/proof/service-' || n)
  ELSE jsonb_build_object('resource_type', 'aws_ecs_task', 'state', 'RUNNING',
    'attributes', jsonb_build_object('cluster_arn',
      'arn:aws:ecs:us-east-1:123456789012:cluster/proof', 'group', 'service:proof')) END
FROM generate_series(1, 30000) AS n`)
	if err != nil {
		t.Fatalf("seed 30,000 ECS facts: %v", err)
	}
}

func assertECSIndexState(t *testing.T, ctx context.Context, database *sql.DB, name string, want bool) {
	t.Helper()
	var definition sql.NullString
	err := database.QueryRowContext(ctx, "SELECT pg_get_indexdef(to_regclass($1))", name).Scan(&definition)
	if err != nil || definition.Valid != want {
		t.Fatalf("index %s present=%t, want %t, err=%v", name, definition.Valid, want, err)
	}
	if want {
		assertECSIndexValidity(t, ctx, database, name, true)
		if !strings.Contains(definition.String, "fact_records") {
			t.Fatalf("index %s definition = %q", name, definition.String)
		}
	}
}

func assertECSIndexValidity(t *testing.T, ctx context.Context, database *sql.DB, name string, want bool) {
	t.Helper()
	var valid, ready bool
	err := database.QueryRowContext(ctx, `SELECT i.indisvalid, i.indisready FROM pg_index i
JOIN pg_class c ON c.oid = i.indexrelid WHERE c.relname = $1`, name).Scan(&valid, &ready)
	if err != nil || valid != want || (want && !ready) {
		t.Fatalf("index %s valid=%t ready=%t, want valid=%t, err=%v", name, valid, ready, want, err)
	}
}
