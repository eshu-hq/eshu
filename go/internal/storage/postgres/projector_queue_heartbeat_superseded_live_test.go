// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// heartbeatProofDB returns a fresh proof schema seeded with scope-hb, whose
// published generation is gen-pub. seedSQL adds the generation under test and
// its running projector row.
func heartbeatProofDB(t *testing.T, seedSQL string) *sql.DB {
	t.Helper()
	dsn := os.Getenv("ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN to a disposable Postgres database")
	}
	database := openLivenessProofDB(t, dsn)
	provisionLivenessSchema(t, database, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES ('scope-hb', 'repository', 'github', 'proof/hb', 'git',
          'proof/hb', now(), now(), 'active', 'gen-pub');
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES ('gen-pub', 'scope-hb', 'push', now() - interval '1 hour',
          now() - interval '1 hour', 'active', now() - interval '1 hour');
`+seedSQL)
	return database
}

// heartbeatRowState is the work and generation state a heartbeat leaves.
type heartbeatRowState struct {
	status, failureClass, generationStatus, publishedStatus, pointer string
	leaseOwner                                                       sql.NullString
	claimUntil, generationSupersededAt                               sql.NullTime
}

func readHeartbeatRowState(t *testing.T, database *sql.DB, generationID string) heartbeatRowState {
	t.Helper()
	var state heartbeatRowState
	var failureClass sql.NullString
	if err := database.QueryRowContext(context.Background(), `
SELECT work.status, work.failure_class, work.lease_owner, work.claim_until,
       generation.status, generation.superseded_at,
       (SELECT status FROM scope_generations WHERE generation_id = 'gen-pub'),
       (SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = 'scope-hb')
FROM fact_work_items AS work
JOIN scope_generations AS generation ON generation.generation_id = work.generation_id
WHERE work.generation_id = $1`, generationID).Scan(
		&state.status, &failureClass, &state.leaseOwner, &state.claimUntil,
		&state.generationStatus, &state.generationSupersededAt,
		&state.publishedStatus, &state.pointer,
	); err != nil {
		t.Fatalf("read heartbeat state for %s: %v", generationID, err)
	}
	state.failureClass = failureClass.String
	return state
}

func heartbeatProofWork(generationID string) projector.ScopeGenerationWork {
	return projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: "scope-hb"},
		Generation:   scope.ScopeGeneration{GenerationID: generationID},
		AttemptCount: 1,
	}
}

func heartbeatFenceCount(t *testing.T, reader interface {
	Collect(context.Context, *metricdata.ResourceMetrics) error
},
) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	return counterTotal(rm, "eshu_dp_superseded_generation_fence_total")
}

// TestProjectorHeartbeatRefusesSupersededGeneration closes the in-flight half
// of #7130. A worker whose generation a newer Ack retired, for example a
// zombie whose lease expired while gen-pub was claimed and acked, must stop at
// its next heartbeat instead of projecting the retired generation until Ack.
// Heartbeat returns ErrWorkSuperseded, which makes the projector service
// cancel the projection, and the retired generation's superseded_at is left
// untouched.
func TestProjectorHeartbeatRefusesSupersededGeneration(t *testing.T) {
	supersededAt := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Microsecond)
	for _, tc := range []struct {
		name       string
		claimUntil string
	}{
		{name: "live_lease", claimUntil: "now() + interval '1 minute'"},
		{name: "expired_lease_zombie", claimUntil: "now() - interval '1 minute'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := heartbeatProofDB(t, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at, superseded_at
) VALUES ('gen-old', 'scope-hb', 'push', now() - interval '2 hours',
          now() - interval '2 hours', 'superseded', now() - interval '2 hours',
          '`+supersededAt.Format(time.RFC3339Nano)+`');
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, payload,
    created_at, updated_at
) VALUES ('projector_scope-hb_gen-old', 'scope-hb', 'gen-old', 'projector',
          'source_local', 'running', 1, 'proof-worker', `+tc.claimUntil+`,
          now(), '{}'::jsonb, now(), now());
`)
			queue := NewProjectorQueue(SQLDB{DB: database}, "proof-worker", time.Minute)
			instruments, reader := newEnqueueInstruments(t)
			queue.Instruments = instruments

			err := queue.Heartbeat(context.Background(), heartbeatProofWork("gen-old"))
			if !errors.Is(err, failure.ErrWorkSuperseded) {
				t.Fatalf("Heartbeat() on a superseded generation = %v, want ErrWorkSuperseded", err)
			}
			state := readHeartbeatRowState(t, database, "gen-old")
			if state.status != "superseded" || state.failureClass != projectorHeartbeatGenerationSupersededClass ||
				state.leaseOwner.Valid || state.claimUntil.Valid {
				t.Fatalf("work = %+v, want superseded with class %s and the lease cleared",
					state, projectorHeartbeatGenerationSupersededClass)
			}
			if state.generationStatus != "superseded" || !state.generationSupersededAt.Time.Equal(supersededAt) ||
				state.publishedStatus != "active" || state.pointer != "gen-pub" {
				t.Fatalf("generations = %+v, want gen-old superseded at %v, gen-pub active and published",
					state, supersededAt)
			}
			if got := heartbeatFenceCount(t, reader); got != 1 {
				t.Fatalf("superseded generation fence count = %d, want 1", got)
			}
		})
	}
}

// TestProjectorHeartbeatRenewsLiveGeneration holds the controls: a heartbeat
// on a live generation with no newer sibling renews the lease, and the older
// newer-pending supersede path keeps its own failure_class and does not count
// as a superseded-generation fence.
func TestProjectorHeartbeatRenewsLiveGeneration(t *testing.T) {
	t.Run("active_no_newer_renews", func(t *testing.T) {
		database := heartbeatProofDB(t, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, payload,
    created_at, updated_at
) VALUES ('projector_scope-hb_gen-pub', 'scope-hb', 'gen-pub', 'projector',
          'source_local', 'running', 1, 'proof-worker', now() + interval '5 seconds',
          now(), '{}'::jsonb, now(), now());
`)
		before := readHeartbeatRowState(t, database, "gen-pub")
		queue := NewProjectorQueue(SQLDB{DB: database}, "proof-worker", time.Minute)
		instruments, reader := newEnqueueInstruments(t)
		queue.Instruments = instruments

		if err := queue.Heartbeat(context.Background(), heartbeatProofWork("gen-pub")); err != nil {
			t.Fatalf("Heartbeat() on the live generation = %v, want nil", err)
		}
		after := readHeartbeatRowState(t, database, "gen-pub")
		if after.status != "running" || !after.claimUntil.Time.After(before.claimUntil.Time) {
			t.Fatalf("work = %+v, want running with claim_until advanced past %v", after, before.claimUntil.Time)
		}
		if got := heartbeatFenceCount(t, reader); got != 0 {
			t.Fatalf("superseded generation fence count = %d, want 0", got)
		}
	})

	t.Run("newer_pending_keeps_its_class", func(t *testing.T) {
		database := heartbeatProofDB(t, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status
) VALUES ('gen-next', 'scope-hb', 'push', now(), now(), 'pending');
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, payload,
    created_at, updated_at
) VALUES ('projector_scope-hb_gen-pub', 'scope-hb', 'gen-pub', 'projector',
          'source_local', 'running', 1, 'proof-worker', now() + interval '1 minute',
          now(), '{}'::jsonb, now(), now());
`)
		queue := NewProjectorQueue(SQLDB{DB: database}, "proof-worker", time.Minute)
		instruments, reader := newEnqueueInstruments(t)
		queue.Instruments = instruments

		if err := queue.Heartbeat(context.Background(), heartbeatProofWork("gen-pub")); !errors.Is(err, failure.ErrWorkSuperseded) {
			t.Fatalf("Heartbeat() with a newer pending generation = %v, want ErrWorkSuperseded", err)
		}
		state := readHeartbeatRowState(t, database, "gen-pub")
		if state.status != "superseded" || state.failureClass != "projector_superseded_by_newer_generation" ||
			state.generationStatus != "active" {
			t.Fatalf("work = %+v, want superseded by the newer generation, gen-pub still active", state)
		}
		if got := heartbeatFenceCount(t, reader); got != 0 {
			t.Fatalf("superseded generation fence count = %d, want 0 for the newer-pending path", got)
		}
	})
}
