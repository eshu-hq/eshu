// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9874 disposable PostgreSQL proof of what ListActivePackageManifestDependencyFacts returns for a git scope whose active generation is a delta: whether unchanged manifest pins of the full base generation stay visible to a reopened impact holder pass; reads ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN

import (
	"context"
	"database/sql"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

func seedDeltaProofFact(t *testing.T, ctx context.Context, database *sql.DB, generationID, factID, name string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
     source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, 'sidecar-live-scope', $2, 'content_entity', $3,
        'git', $1, clock_timestamp(), clock_timestamp(), FALSE,
        jsonb_build_object(
            'repo_id', 'repository:sidecar-live',
            'entity_type', 'Variable',
            'entity_name', $4::text,
            'entity_metadata', jsonb_build_object(
                'config_kind', 'dependency', 'package_manager', 'npm')))`,
		factID, generationID, name, name); err != nil {
		t.Fatalf("seed %s: %v", factID, err)
	}
}

// TestListActivePackageManifestDependencyFactsDeltaActiveScopeLive is the
// #9874 precondition experiment. A full generation holds two pins (alpha,
// bravo); a delta generation re-emits only alpha and becomes the scope's
// active generation. A reopened impact holder pass reads manifest rows through
// this loader, so the unchanged bravo pin must stay visible. If it does not,
// the pass would retract bravo's findings.
func TestListActivePackageManifestDependencyFactsDeltaActiveScopeLive(t *testing.T) {
	dsn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN")
	optIn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE")
	ctx, database := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 2*time.Minute)
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	seedPackageManifestConsumptionBackfillScope(t, ctx, database) // generation 'sidecar-live-generation', active
	if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations
    (scope_id, generation_id, trigger_kind, is_delta, observed_at, ingested_at, status)
VALUES ('sidecar-live-scope', 'sidecar-live-delta', 'sync', TRUE,
        clock_timestamp(), clock_timestamp(), 'pending')`); err != nil {
		t.Fatalf("seed delta generation: %v", err)
	}
	seedDeltaProofFact(t, ctx, database, "sidecar-live-generation", "full-alpha", "alpha")
	seedDeltaProofFact(t, ctx, database, "sidecar-live-generation", "full-bravo", "bravo")
	seedDeltaProofFact(t, ctx, database, "sidecar-live-delta", "delta-alpha", "alpha")
	// Activating the delta supersedes the full base, as the ingestion store does.
	if _, err := database.ExecContext(ctx, `
UPDATE scope_generations SET status = 'superseded' WHERE generation_id = 'sidecar-live-generation';
UPDATE scope_generations SET status = 'active' WHERE generation_id = 'sidecar-live-delta';
UPDATE ingestion_scopes SET active_generation_id = 'sidecar-live-delta'
WHERE scope_id = 'sidecar-live-scope'`); err != nil {
		t.Fatalf("activate delta: %v", err)
	}

	loaded, err := NewFactStore(SQLDB{DB: database}).ListActivePackageManifestDependencyFacts(
		ctx, []string{"npm"}, []string{"alpha", "bravo"},
	)
	if err != nil {
		t.Fatalf("ListActivePackageManifestDependencyFacts(): %v", err)
	}
	var got []string
	for _, envelope := range loaded {
		got = append(got, envelope.FactID)
	}
	sort.Strings(got)
	t.Logf("loaded manifest facts for delta-active scope = %v", got)
	want := []string{"delta-alpha", "full-bravo"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("loaded facts = %v, want %v (the unchanged full-base pin must stay visible under an active delta)", got, want)
	}
}
