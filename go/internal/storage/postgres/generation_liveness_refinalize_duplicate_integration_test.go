// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// livenessDuplicateProjectorSeedSQL seeds one wedged active generation (aged
// 2h, one outstanding non-repo_dependency shared intent) that carries TWO
// projector/source_local work items for the same (scope_id, generation_id):
// the canonical projector_<scope>_<gen> row and the refinalize_<scope>_<gen>
// row that the rebuild-from-facts refinalize prelude inserts (see
// rebuild/reset/refinalize.go). fact_work_items has no unique index over
// (stage, domain, scope_id, generation_id), so the pair is legal data.
//
// canonicalPayload is the canonical row's payload JSON; refinalizeStatus is
// the refinalize row's status.
func livenessDuplicateProjectorSeedSQL(canonicalPayload, refinalizeStatus string) string {
	return fmt.Sprintf(`
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES (
    'scope-dup', 'repository', 'github', 'acme/dup', 'git',
    'acme/dup', now(), now(), 'active', 'gen-dup'
);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES (
    'gen-dup', 'scope-dup', 'push',
    now() - interval '2 hours', now() - interval '2 hours',
    'active', now() - interval '2 hours'
);
INSERT INTO shared_projection_intents (
    intent_id, projection_domain, partition_key, scope_id,
    acceptance_unit_id, repository_id, source_run_id, generation_id,
    payload, created_at
) VALUES (
    'intent-dup', 'graph', 'acme/dup', 'scope-dup',
    '', 'acme/dup', 'run-dup', 'gen-dup',
    '{"action":"sync"}'::jsonb, now() - interval '2 hours'
);
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status, payload,
    created_at, updated_at
) VALUES
    (
        'projector_scope-dup_gen-dup', 'scope-dup', 'gen-dup',
        'projector', 'source_local', 'succeeded', '%s'::jsonb,
        now() - interval '2 hours', now() - interval '2 hours'
    ),
    (
        'refinalize_scope-dup_gen-dup', 'scope-dup', 'gen-dup',
        'projector', 'source_local', '%s', '{}'::jsonb,
        now() - interval '1 hour', now() - interval '1 hour'
    );
`, canonicalPayload, refinalizeStatus)
}

// TestGenerationLivenessRefinalizeDuplicateProjectorRow proves the wedged
// recovery sweep tolerates more than one projector/source_local work item per
// generation. Before the fix, the recovery-budget lookup was a bare scalar
// subquery over (stage, domain, scope_id, generation_id) and failed the whole
// sweep with SQLSTATE 21000 ("more than one row returned by a subquery used as
// an expression") once recover-generations/refinalize had inserted a
// refinalize_* sibling next to the canonical projector_* row. One such
// generation starved recovery for every scope. Set
// ESHU_GENERATION_LIVENESS_PROOF_DSN to run it; it is skipped otherwise.
func TestGenerationLivenessRefinalizeDuplicateProjectorRow(t *testing.T) {
	dsn := os.Getenv("ESHU_GENERATION_LIVENESS_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_GENERATION_LIVENESS_PROOF_DSN to run the generation liveness integration proof")
	}
	policy := GenerationLivenessPolicy{
		ActivationDeadline: 30 * time.Minute,
		MaxRecoverAttempts: 2,
		BatchLimit:         100,
	}

	tests := []struct {
		name             string
		canonicalPayload string
		refinalizeStatus string
		wantRecovered    int
		wantAttempts     int // canonical row's liveness_recovery_attempts after the sweep
	}{
		{
			name:             "sibling succeeded row does not fail the sweep and canonical row is re-driven",
			canonicalPayload: `{}`,
			refinalizeStatus: "succeeded",
			wantRecovered:    1,
			wantAttempts:     1,
		},
		{
			name:             "budget read from the canonical row still increments across a duplicate",
			canonicalPayload: `{"liveness_recovery_attempts":1}`,
			refinalizeStatus: "succeeded",
			wantRecovered:    1,
			wantAttempts:     2,
		},
		{
			name:             "exhausted budget on the canonical row blocks re-drive despite a counterless sibling",
			canonicalPayload: `{"liveness_recovery_attempts":2}`,
			refinalizeStatus: "succeeded",
			wantRecovered:    0,
			wantAttempts:     2,
		},
		{
			name:             "pending refinalize sibling is in-flight work and blocks re-drive",
			canonicalPayload: `{}`,
			refinalizeStatus: "pending",
			wantRecovered:    0,
			wantAttempts:     0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openLivenessProofDB(t, dsn)
			provisionLivenessSchema(t, db, livenessDuplicateProjectorSeedSQL(tt.canonicalPayload, tt.refinalizeStatus))
			store := NewGenerationLivenessStore(SQLDB{DB: db})
			ctx := context.Background()

			result, err := store.RecoverWedgedGenerations(ctx, policy, time.Now().UTC())
			if err != nil {
				t.Fatalf("RecoverWedgedGenerations() error = %v, want nil", err)
			}
			if result.Recovered != tt.wantRecovered {
				t.Fatalf("Recovered = %d, want %d", result.Recovered, tt.wantRecovered)
			}

			var attempts int
			if err := db.QueryRowContext(ctx, `
SELECT COALESCE((payload ->> 'liveness_recovery_attempts')::int, 0)
FROM fact_work_items
WHERE work_item_id = 'projector_scope-dup_gen-dup'`).Scan(&attempts); err != nil {
				t.Fatalf("read canonical work item: %v", err)
			}
			if attempts != tt.wantAttempts {
				t.Fatalf("canonical liveness_recovery_attempts = %d, want %d", attempts, tt.wantAttempts)
			}

			var rowCount int
			if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM fact_work_items
WHERE stage = 'projector' AND domain = 'source_local'
  AND scope_id = 'scope-dup' AND generation_id = 'gen-dup'`).Scan(&rowCount); err != nil {
				t.Fatalf("count projector rows: %v", err)
			}
			if rowCount != 2 {
				t.Fatalf("projector/source_local rows = %d, want 2 (sweep must not add a third)", rowCount)
			}
		})
	}
}
