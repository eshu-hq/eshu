// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// terraformLiveSelectionReader wraps the production status store. It pins the
// snapshot clock so two reads render identical snapshot timestamps (a route's
// own handler-clock fields are dropped by statusBodiesEqual), records how many
// Terraform rows each read returned, and can override the route's Terraform
// choice to build the full-read baseline (forceRead) or a seeded violation
// (forceSkip).
type terraformLiveSelectionReader struct {
	inner         statuspkg.Reader
	asOf          time.Time
	forceRead     bool
	forceSkip     bool
	terraformRows int
	reads         int
}

func (r *terraformLiveSelectionReader) ReadStatusSnapshot(ctx context.Context, asOf time.Time) (statuspkg.RawSnapshot, error) {
	return r.ReadStatusSnapshotFiltered(ctx, asOf, statuspkg.FullSnapshotSelection())
}

func (r *terraformLiveSelectionReader) ReadStatusSnapshotFiltered(
	ctx context.Context,
	_ time.Time,
	selection statuspkg.SnapshotSelection,
) (statuspkg.RawSnapshot, error) {
	if r.forceRead {
		selection.SkipTerraformStateEvidence = false
	}
	if r.forceSkip {
		selection.SkipTerraformStateEvidence = true
	}
	raw, err := r.inner.ReadStatusSnapshotFiltered(ctx, r.asOf, selection)
	r.reads++
	r.terraformRows = len(raw.TerraformStateLastSerials) + len(raw.TerraformStateRecentWarnings)
	return raw, err
}

// TestStatusRoutesTerraformSelectionLive proves on PostgreSQL 18 with the full
// bootstrap schema and seeded Terraform serials and warnings (#7009) that every
// Terraform-free status route returns byte-identical responses with and without
// the Terraform-state reads, that the routes that render terraform_state still
// carry the seeded rows, and that forcing the skip onto a rendering route
// changes its response (the equality probe can fail).
//
// Run with a disposable PostgreSQL 18 administrative database:
//
//	ESHU_STATUS_TERRAFORM_SELECTION_PROOF_DSN=postgres://user:pass@127.0.0.1:<port>/postgres?sslmode=disable \
//	ESHU_STATUS_TERRAFORM_SELECTION_PROOF_DISPOSABLE=1 \
//	go test ./internal/query -run TestStatusRoutesTerraformSelectionLive -count=1
func TestStatusRoutesTerraformSelectionLive(t *testing.T) {
	dsn := os.Getenv("ESHU_STATUS_TERRAFORM_SELECTION_PROOF_DSN")
	optIn := os.Getenv("ESHU_STATUS_TERRAFORM_SELECTION_PROOF_DISPOSABLE")
	ctx, db := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 3*time.Minute)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	asOf := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	seedTerraformStatusEvidence(ctx, t, db, asOf)
	store := storagepostgres.NewStatusStore(storagepostgres.SQLQueryer{DB: db})

	// The seed yields one serial and four warning rows (three tfstate, one git).
	const seededTerraformRows = 5
	for _, route := range terraformFreeStatusRoutes {
		baseline := &terraformLiveSelectionReader{inner: store, asOf: asOf, forceRead: true}
		selected := &terraformLiveSelectionReader{inner: store, asOf: asOf}
		want := serveStatusRoute(t, baseline, route.path).Body.Bytes()
		got := serveStatusRoute(t, selected, route.path).Body.Bytes()
		if baseline.reads != 1 || selected.reads != 1 {
			t.Fatalf("%s: reads baseline=%d selected=%d, want 1/1", route.path, baseline.reads, selected.reads)
		}
		if baseline.terraformRows != seededTerraformRows || selected.terraformRows != 0 {
			t.Fatalf("%s: Terraform rows baseline=%d selected=%d, want %d/0", route.path, baseline.terraformRows, selected.terraformRows, seededTerraformRows)
		}
		if !statusBodiesEqual(t, route.path, got, want) {
			t.Fatalf("%s: response changed without Terraform evidence:\nfull=%s\nskipped=%s", route.path, want, got)
		}
	}
	for _, route := range terraformRenderingStatusRoutes {
		full := &terraformLiveSelectionReader{inner: store, asOf: asOf}
		body := serveStatusRoute(t, full, route.path).Body.Bytes()
		if full.terraformRows != seededTerraformRows {
			t.Fatalf("%s: Terraform rows = %d, want %d", route.path, full.terraformRows, seededTerraformRows)
		}
		for _, marker := range []string{`"terraform_state"`, `"hash-live-a"`, `"state_missing"`, `"unresolved_backend_expression"`, `"repo-live:infra/backend.tf"`} {
			if !strings.Contains(string(body), marker) {
				t.Fatalf("%s: response lost %s: %s", route.path, marker, body)
			}
		}
		violation := serveStatusRoute(t, &terraformLiveSelectionReader{inner: store, asOf: asOf, forceSkip: true}, route.path).Body.Bytes()
		if bytes.Equal(body, violation) {
			t.Fatalf("%s: equality probe cannot see a lost terraform_state section", route.path)
		}
	}
}

// seedTerraformStatusEvidence seeds one Terraform state scope with an active
// and a superseded generation, three state warnings, and one Git repository
// scope with an unresolved backend expression warning.
func seedTerraformStatusEvidence(ctx context.Context, t *testing.T, db *sql.DB, asOf time.Time) {
	t.Helper()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, query)
		}
	}
	const (
		tfScope      = "tfstate-live-a"
		tfActive     = "terraform_state:state_snapshot:s3:hash-live-a:lineage-live:serial:7"
		tfSuperseded = "terraform_state:state_snapshot:s3:hash-live-a:lineage-live:serial:6"
		gitScope     = "git-repo-live"
		gitActive    = "git-repo-live-gen-1"
	)
	exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'state_snapshot', 'terraform_state', $1, 'terraform_state', $1, $3, $3, 'active', $2,
  '{"locator_hash":"hash-live-a","backend_kind":"s3"}'::jsonb)`, tfScope, tfActive, asOf.Add(-time.Hour))
	exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'repository', 'git', $1, 'git', $1, $3, $3, 'active', $2, '{}'::jsonb)`, gitScope, gitActive, asOf.Add(-time.Hour))
	for _, gen := range []struct {
		id, scope, status string
		ingested          time.Duration
	}{
		{tfSuperseded, tfScope, "superseded", -3 * time.Hour},
		{tfActive, tfScope, "active", -2 * time.Hour},
		{gitActive, gitScope, "active", -2 * time.Hour},
	} {
		exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
VALUES ($1, $2, 'snapshot', $3, $3, $4, '{}'::jsonb)`, gen.id, gen.scope, asOf.Add(gen.ingested), gen.status)
	}
	for _, warning := range []struct {
		id, scope, generation, payload string
		observed                       time.Duration
	}{
		{"warn-tf-1", tfScope, tfSuperseded, `{"warning_kind":"state_missing","reason":"older serial","severity":"warning","source":"s3"}`, -3 * time.Hour},
		{"warn-tf-2", tfScope, tfActive, `{"warning_kind":"state_missing","reason":"object absent","severity":"warning","source":"s3"}`, -2 * time.Hour},
		{"warn-tf-3", tfScope, tfActive, `{"warning_kind":"state_in_vcs","reason":"committed state","severity":"info","source":"git_local_file","source_handle":"state_snapshot:s3:hash-live-a"}`, -2 * time.Hour},
		{"warn-git-1", gitScope, gitActive, `{"warning_kind":"unresolved_backend_expression","reason":"var bucket","severity":"warning","source":"git","repo_id":"repo-live","source_path":"infra/backend.tf"}`, -2 * time.Hour},
	} {
		exec(`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
  source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'terraform_state_warning', $1, 'terraform_state', $1, $4, $4, $5::jsonb)`,
			warning.id, warning.scope, warning.generation, asOf.Add(warning.observed), warning.payload)
	}
}
