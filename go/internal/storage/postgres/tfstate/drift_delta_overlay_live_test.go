// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tfstate

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/correlation/drift/tfconfigstate"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestDriftReadsLiveBaselineSingleGeneration guards the legacy
// active-only drift readers: on a scope with no delta-active
// generation, the backend anchor resolves, config addresses load, and
// no removed_from_config candidate appears. The AWS runtime-drift
// consumer and unit fakes stay on these methods; the delta-overlay
// proof for the same repro lives in the parent package
// (TestTerraformDriftOverlayReadsLive, issue #10006), which caught the
// RED on these methods first (delta-active: 0 canonical rows vs 1, 0
// config addresses vs 2, 2 false removed_from_config vs 0).
//
// Run with: ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu
//
//	go test ./internal/storage/postgres/tfstate -run 'TestDriftReadsLiveBaselineSingleGeneration' -count=1 -v
func TestDriftReadsLiveBaselineSingleGeneration(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the drift active-only baseline proof")
	}
	ctx := context.Background()
	sqlDB := openDriftDeltaOverlayLiveSchema(t, ctx, dsn)
	counter := &driftDeltaCountingQueryer{db: sqlDB}
	seedDriftDeltaOverlayLiveFixture(t, ctx, sqlDB)

	const (
		configScope = "repository:repo-drift"
		repoID      = "repo-drift"
	)
	bucket, key := "drift-bucket", "envs/prod.tfstate"
	hash := terraformstate.ScopeLocatorHash(terraformstate.BackendS3, "s3://"+bucket+"/"+key)
	stateScope := "state_snapshot:s3:" + hash

	backendQuery := PostgresTerraformBackendQuery{DB: counter}
	rows, err := backendQuery.ListTerraformBackendsByLocator(ctx, "s3", hash)
	if err != nil {
		t.Fatalf("ListTerraformBackendsByLocator() error = %v", err)
	}
	if got := len(rows); got != 1 {
		t.Fatalf("backend-canonical rows = %d, want 1", got)
	}
	if rows[0].RepoID != repoID || rows[0].CommitID != "gen-full" {
		t.Fatalf("canonical row = (%q, %q), want (%q, gen-full)", rows[0].RepoID, rows[0].CommitID, repoID)
	}

	loader := PostgresDriftEvidenceLoader{DB: counter}
	anchor := tfstatebackend.CommitAnchor{RepoID: repoID, ScopeID: configScope, CommitID: "gen-full"}
	configByAddress, err := loader.LoadConfigByAddress(ctx, configScope, "gen-full", modulePrefixMap{}, nil)
	if err != nil {
		t.Fatalf("LoadConfigByAddress() error = %v", err)
	}
	if got := len(configByAddress); got != 2 {
		t.Fatalf("config addresses = %d, want 2", got)
	}

	addressed, err := loader.LoadDriftEvidence(ctx, stateScope, anchor)
	if err != nil {
		t.Fatalf("LoadDriftEvidence() error = %v", err)
	}
	if got := countDriftKindCandidates(tfconfigstate.BuildCandidates(addressed, anchor, stateScope), "removed_from_config"); got != 0 {
		t.Fatalf("removed_from_config candidates = %d, want 0", got)
	}
	t.Logf("%d statements\n%s", counter.count(), counter.fingerprint())
}

// countDriftKindCandidates counts candidates carrying a drift_kind evidence
// atom with the given value. The atom is the downstream-visible label for
// the classification (candidate.go's drift-kind atom); matching it keeps the
// test independent of candidate ID formatting.
func countDriftKindCandidates(candidates []model.Candidate, wantKind string) int {
	count := 0
	for _, candidate := range candidates {
		for _, atom := range candidate.Evidence {
			if atom.EvidenceType == tfconfigstate.EvidenceTypeDriftKind && atom.Value == wantKind {
				count++
				break
			}
		}
	}
	return count
}

// driftDeltaCountingQueryer wraps *sql.DB, counting every QueryContext call
// so the test logs the exact statement mix.
type driftDeltaCountingQueryer struct {
	db *sql.DB

	mu      sync.Mutex
	queries []string
}

func (q *driftDeltaCountingQueryer) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	q.mu.Lock()
	q.queries = append(q.queries, query)
	q.mu.Unlock()
	return q.db.QueryContext(ctx, query, args...)
}

func (q *driftDeltaCountingQueryer) count() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.queries)
}

// fingerprint returns one line per statement with its reader label, so
// the log shows the statement mix without pasting full query texts.
func (q *driftDeltaCountingQueryer) fingerprint() string {
	q.mu.Lock()
	defer q.mu.Unlock()
	var lines []string
	for i, query := range q.queries {
		lines = append(lines, fmt.Sprintf("  [%d] %s", i+1, driftDeltaStatementLabel(query)))
	}
	return strings.Join(lines, "\n")
}

// driftDeltaStatementLabel names a recorded statement by the distinctive
// fragment of the reader query it ran.
func driftDeltaStatementLabel(query string) string {
	switch {
	case strings.Contains(query, "backend_generations"):
		return "canonical backend rows"
	case strings.Contains(query, "prior_generations"):
		return "prior-config addresses"
	case strings.Contains(query, "terraform_modules"):
		return "module calls"
	case strings.Contains(query, "terraform_resources"):
		return "config resources"
	case strings.Contains(query, "terraform_state_snapshot"):
		return "state snapshot"
	case strings.Contains(query, "terraform_state_resource"):
		return "state resources"
	default:
		first := strings.TrimSpace(query)
		if idx := strings.Index(first, "\n"); idx >= 0 {
			first = first[:idx]
		}
		return "other: " + first
	}
}

// openDriftDeltaOverlayLiveSchema mirrors the package's established
// live-Postgres proof pattern (throwaway schema, single connection, the
// shipped migration bytes): the three base tables plus the two delta-column
// migrations the overlay rule reads (148 baseline, 210 union base).
func openDriftDeltaOverlayLiveSchema(t *testing.T, ctx context.Context, dsn string) *sql.DB {
	t.Helper()
	schemaName := fmt.Sprintf("drift_delta_overlay_live_%d", time.Now().UnixNano())
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	if _, err := sqlDB.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create drift delta overlay live schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = sqlDB.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	if _, err := sqlDB.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	for _, defName := range []string{
		"ingestion_scopes",
		"scope_generations",
		"fact_records",
		"scope_generations_delta_baseline_commit_sha",
		"scope_generations_delta_union_base_commit_sha",
	} {
		if _, err := sqlDB.ExecContext(ctx, tfstateMigrationSQL(t, defName)); err != nil {
			t.Fatalf("apply migration %q: %v", defName, err)
		}
	}
	return sqlDB
}

// seedDriftDeltaOverlayLiveFixture seeds the #10006 repro: a config scope
// whose full generation (gen-full, commit-full) holds backend.tf plus
// main.tf with two resources, a pending delta (gen-delta) touching only
// outputs.tf, and an aligned state scope. The delta stays pending here;
// activating it is the overlay proof's job in the parent package.
func seedDriftDeltaOverlayLiveFixture(t *testing.T, ctx context.Context, sqlDB *sql.DB) {
	t.Helper()
	now := time.Now().UTC()
	exec := func(what, statement string, args ...any) {
		t.Helper()
		if _, err := sqlDB.ExecContext(ctx, statement, args...); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}

	exec("seed config scope", `
INSERT INTO ingestion_scopes
    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
VALUES ('repository:repo-drift', 'repository', 'git', 'repo-drift', 'git', 'repo-drift', $1, $1, 'active', 'gen-full')`, now)
	exec("seed state scope", `
INSERT INTO ingestion_scopes
    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
VALUES ($1, 'state_snapshot', 'terraform-state', $1, 'terraform-state', $1, $2, $2, 'active', 'st-gen-1')`,
		"state_snapshot:s3:"+terraformstate.ScopeLocatorHash(terraformstate.BackendS3, "s3://drift-bucket/envs/prod.tfstate"), now)

	exec("seed gen-full", `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, source_commit_sha, observed_at, ingested_at, activated_at, status)
VALUES ('gen-full', 'repository:repo-drift', 'sync', 'commit-full', $1, $1, $1, 'active')`, now)
	exec("seed gen-delta pending", `
INSERT INTO scope_generations
    (generation_id, scope_id, trigger_kind, is_delta, source_commit_sha,
     delta_baseline_commit_sha, delta_union_base_commit_sha,
     observed_at, ingested_at, status)
VALUES ('gen-delta', 'repository:repo-drift', 'sync', true, 'commit-delta',
        'commit-full', 'commit-full', $1, $1, 'pending')`, now)
	exec("seed st-gen-1", `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('st-gen-1', $1, 'sync', $2, $2, 'active')`,
		"state_snapshot:s3:"+terraformstate.ScopeLocatorHash(terraformstate.BackendS3, "s3://drift-bucket/envs/prod.tfstate"), now)

	insertFileFact := func(factID, generationID, stableKey, payload string) {
		t.Helper()
		exec("seed fact "+factID, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, 'repository:repo-drift', $2, 'file', $3, 'git', $3, $4, $4, $5::jsonb)`,
			factID, generationID, stableKey, now, payload)
	}

	backendEntry := `{"backend_kind":"s3","bucket":"drift-bucket","bucket_is_literal":true,` +
		`"key":"envs/prod.tfstate","key_is_literal":true,"region":"us-east-1","region_is_literal":true}`
	insertFileFact("fact-full-repo", "gen-full", "repository:repo-drift",
		`{"repo_id":"repo-drift","local_path":"/repo"}`)
	insertFileFact("fact-full-backend", "gen-full", "file:repo-drift:backend.tf",
		`{"repo_id":"repo-drift","relative_path":"backend.tf","parsed_file_data":`+
			`{"terraform_backends":[`+backendEntry+`],"terraform_variables":[],"terraform_locals":[]}}`)
	insertFileFact("fact-full-main", "gen-full", "file:repo-drift:main.tf",
		`{"repo_id":"repo-drift","relative_path":"main.tf","parsed_file_data":`+
			`{"terraform_resources":[{"resource_type":"aws_instance","resource_name":"a","path":"main.tf"},`+
			`{"resource_type":"aws_instance","resource_name":"b","path":"main.tf"}]}}`)

	// The delta touches only outputs.tf: no resources, no modules, no
	// backends. Its repository fact carries the delta_generation marker the
	// overlay parses for the touched set.
	insertFileFact("fact-delta-repo", "gen-delta", "repository:repo-drift",
		`{"repo_id":"repo-drift","local_path":"/repo",`+
			`"delta_generation":true,"delta_relative_paths":["outputs.tf"],"delta_deleted_relative_paths":[]}`)
	insertFileFact("fact-delta-outputs", "gen-delta", "file:repo-drift:outputs.tf",
		`{"repo_id":"repo-drift","relative_path":"outputs.tf","parsed_file_data":`+
			`{"terraform_resources":[],"terraform_modules":[]}}`)

	stateScope := "state_snapshot:s3:" + terraformstate.ScopeLocatorHash(terraformstate.BackendS3, "s3://drift-bucket/envs/prod.tfstate")
	exec("seed state snapshot", `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ('fact-state-snapshot', $1, 'st-gen-1', 'terraform_state_snapshot', 'snapshot:st-gen-1',
        'terraform-state', 'snapshot:st-gen-1', $2, $2, '{"lineage":"drift-lineage-1","serial":0}'::jsonb)`, stateScope, now)
	for _, name := range []string{"a", "b"} {
		exec("seed state resource "+name, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, 'st-gen-1', 'terraform_state_resource', $3, 'terraform-state', $3, $4, $4, $5::jsonb)`,
			"fact-state-"+name, stateScope, "state:aws_instance."+name, now,
			`{"address":"aws_instance.`+name+`","type":"aws_instance","attributes":{}}`)
	}
}
