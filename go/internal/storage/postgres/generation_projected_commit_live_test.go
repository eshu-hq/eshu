// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"
)

// projectedCommitProofIndexesSQL adds the migration-002 scope_generations
// indexes the baseline and reconcile reads rely on to the liveness proof
// schema, so the live tests run against the production index shapes and the
// unique one-active-generation-per-scope constraint.
const projectedCommitProofIndexesSQL = `
CREATE INDEX scope_generations_scope_idx
    ON scope_generations (scope_id, status, ingested_at DESC);
CREATE INDEX scope_generations_scope_latest_lookup_idx
    ON scope_generations (scope_id, ingested_at DESC, generation_id DESC);
CREATE UNIQUE INDEX scope_generations_active_scope_idx
    ON scope_generations (scope_id)
    WHERE status = 'active';
`

// proofGeneration is one scope_generations row seeded by the live baseline and
// reconcile-state proofs.
type proofGeneration struct {
	id          string
	isDelta     bool
	status      string
	sha         string
	ingestedAt  time.Time
	activatedAt *time.Time
}

// openProjectedCommitProof provisions an isolated proof schema with one git
// scope and the given generations, and returns an IngestionStore bound to it.
// It skips unless ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN names a disposable
// Postgres database.
func openProjectedCommitProof(t *testing.T, scopeID string, generations []proofGeneration) (IngestionStore, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN to a disposable Postgres database")
	}
	database := openLivenessProofDB(t, dsn)
	provisionLivenessSchema(t, database, projectedCommitProofIndexesSQL)
	ctx := context.Background()
	if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status
) VALUES ($1, 'repository', 'git', $1, 'git', $1, now(), now(), 'active')`, scopeID); err != nil {
		t.Fatalf("seed scope: %v", err)
	}
	for _, generation := range generations {
		var activatedAt any
		if generation.activatedAt != nil {
			activatedAt = *generation.activatedAt
		}
		if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, source_commit_sha, is_delta,
    observed_at, ingested_at, status, activated_at
) VALUES ($1, $2, 'snapshot', $3, $4, $5, $5, $6, $7)`,
			generation.id, scopeID, generation.sha, generation.isDelta,
			generation.ingestedAt, generation.status, activatedAt,
		); err != nil {
			t.Fatalf("seed generation %s: %v", generation.id, err)
		}
	}
	return NewIngestionStore(SQLDB{DB: database}), database
}

func proofTime(value time.Time) *time.Time { return &value }

// TestLastProjectedCommitSHAUsesActiveGenerationLive proves the delta baseline
// is the commit of the scope's active generation (#7317). A generation that
// was superseded while still pending never reached the graph, so a baseline on
// its commit would skip the changes between the active commit and it.
func TestLastProjectedCommitSHAUsesActiveGenerationLive(t *testing.T) {
	base := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name        string
		generations []proofGeneration
		want        string
	}{
		{
			name: "superseded while pending does not become the baseline",
			generations: []proofGeneration{
				{id: "gen-a", status: "active", sha: "sha-a", ingestedAt: base.Add(-2 * time.Hour), activatedAt: proofTime(base.Add(-2 * time.Hour))},
				{id: "gen-b", status: "superseded", sha: "sha-b", ingestedAt: base.Add(-time.Hour)},
			},
			want: "sha-a",
		},
		{
			name: "active delta generation is the baseline",
			generations: []proofGeneration{
				{id: "gen-full", status: "superseded", sha: "sha-full", ingestedAt: base.Add(-3 * time.Hour), activatedAt: proofTime(base.Add(-3 * time.Hour))},
				{id: "gen-delta", isDelta: true, status: "active", sha: "sha-delta", ingestedAt: base.Add(-time.Hour), activatedAt: proofTime(base.Add(-time.Hour))},
			},
			want: "sha-delta",
		},
		{
			name: "no active generation falls back to a full snapshot",
			generations: []proofGeneration{
				{id: "gen-old", status: "superseded", sha: "sha-old", ingestedAt: base.Add(-3 * time.Hour), activatedAt: proofTime(base.Add(-3 * time.Hour))},
				{id: "gen-failed", status: "failed", sha: "sha-failed", ingestedAt: base.Add(-time.Hour)},
			},
			want: "",
		},
		{
			name: "only a pending generation has no baseline",
			generations: []proofGeneration{
				{id: "gen-pending", status: "pending", sha: "sha-pending", ingestedAt: base.Add(-time.Hour)},
			},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scopeID := "git-repository-scope:proof/baseline"
			store, _ := openProjectedCommitProof(t, scopeID, tc.generations)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			got, err := store.LastProjectedCommitSHA(ctx, scopeID)
			if err != nil {
				t.Fatalf("LastProjectedCommitSHA() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("LastProjectedCommitSHA() = %q, want %q", got, tc.want)
			}
		})
	}
}
