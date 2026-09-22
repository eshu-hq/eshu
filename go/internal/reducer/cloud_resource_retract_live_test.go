// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/cloudjoin"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// recordingRetractProbe records the candidate uids the handler hands to the
// retract seam without touching a graph.
type recordingRetractProbe struct {
	calls int
	uids  []string
}

func (r *recordingRetractProbe) RetractDeadCloudResourceNodes(_ context.Context, uids []string, _ string) (int, error) {
	r.calls++
	r.uids = append(r.uids, uids...)
	return len(uids), nil
}

func (r *recordingRetractProbe) WriteCloudResourceNodes(_ context.Context, _ []map[string]any, _ string) error {
	return nil
}

type noopPhaseProbe struct{}

func (noopPhaseProbe) PublishGraphProjectionPhases(_ context.Context, _ []gpphase.PhaseState) error {
	return nil
}

// TestAWSRetractSkipsNewerGenerationUIDsLive is the #6892 P1 intent-level
// proof over a real Postgres: scope active=gen7 with a pending gen8 row,
// then a re-run gen7 intent. The fixed strictly-older prior resolves gen6,
// so the diff yields exactly the gen6 predecessor-only uid and the
// gen8-admitted uid never reaches the retract seam. The old newest-other
// predicate resolved gen8 and handed both uids over.
//
// The current generation is empty on purpose: with zero writes the node
// writer, presence publish, and refresh gate stay untouched, isolating the
// prior-resolution/diff chain. The production retracter itself is proven by
// TestBuildReducerServiceWiresCloudRetractSeams plus the live end-to-end
// proof; here a recording probe observes the candidate set.
//
// Skipped by default; set ESHU_CLOUD_RETRACT_PROVE_LIVE=1 and
// ESHU_CLOUD_RETRACT_PG_DSN.
func TestAWSRetractSkipsNewerGenerationUIDsLive(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_CLOUD_RETRACT_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_CLOUD_RETRACT_PROVE_LIVE=1 (+ ESHU_CLOUD_RETRACT_PG_DSN) to run the newer-generation survival proof")
	}
	dsn := strings.TrimSpace(os.Getenv("ESHU_CLOUD_RETRACT_PG_DSN"))
	if dsn == "" {
		t.Fatal("ESHU_CLOUD_RETRACT_PG_DSN is required")
	}
	ctx := context.Background()

	rawDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = rawDB.Close() }()
	sqldb := postgres.SQLDB{DB: rawDB}

	prefix := fmt.Sprintf("survive-live-%d", time.Now().UnixNano())
	now := time.Now().UTC().Truncate(time.Millisecond)
	scope := prefix + "-scope"
	gen6, gen7, gen8 := prefix+"-gen6", prefix+"-gen7", prefix+"-gen8"

	execSQL := func(query string, args ...any) {
		t.Helper()
		if _, err := rawDB.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("exec: %v", err)
		}
	}
	execSQL(`
INSERT INTO ingestion_scopes
  (scope_id, scope_kind, source_system, source_key, collector_kind,
   partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'aws', 'aws', $1, 'aws', $1, $2, $2, 'active', $3, '{}'::jsonb)
ON CONFLICT (scope_id) DO UPDATE SET active_generation_id = EXCLUDED.active_generation_id`,
		scope, now, gen7)
	seedGeneration := func(gen string, at time.Time, status string) {
		t.Helper()
		execSQL(`
INSERT INTO scope_generations
  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'manual', $3, $3, $4)
ON CONFLICT (generation_id) DO UPDATE SET observed_at = EXCLUDED.observed_at, status = EXCLUDED.status`,
			gen, scope, at, status)
	}
	seedGeneration(gen6, now.Add(-2*time.Hour), "superseded")
	seedGeneration(gen7, now.Add(-1*time.Hour), "active")
	seedGeneration(gen8, now, "pending")

	seedFact := func(gen, key, resourceID string) {
		t.Helper()
		execSQL(`
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
   source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, 'aws_resource', $4, '1.0.0',
  'aws', $4, $5, $5, FALSE,
  ('{"account_id": "111122223333", "region": "us-east-1", "resource_type": "aws_ec2_vpc", "resource_id": "' || $6 || '"}')::jsonb)`,
			prefix+"-fact-"+key, scope, gen, key, now, resourceID)
	}
	seedFact(gen6, "k-live", "vpc-live")
	seedFact(gen8, "k-live-8", "vpc-live")
	seedFact(gen8, "k-new", "vpc-new")

	defer func() {
		execSQL(`DELETE FROM fact_records WHERE scope_id = $1`, scope)
		execSQL(`DELETE FROM scope_generations WHERE scope_id = $1`, scope)
		execSQL(`DELETE FROM ingestion_scopes WHERE scope_id = $1`, scope)
	}()

	probe := &recordingRetractProbe{}
	handler := reducer.AWSResourceMaterializationHandler{
		FactLoader:      postgres.NewFactStore(sqldb),
		NodeWriter:      probe,
		NodeRetracter:   probe,
		PriorGeneration: postgres.NewPriorGenerationID(sqldb),
		PhasePublisher:  noopPhaseProbe{},
	}
	result, err := handler.Handle(ctx, reducer.Intent{
		IntentID:     prefix + "-intent",
		ScopeID:      scope,
		GenerationID: gen7,
		Domain:       reducer.DomainAWSResourceMaterialization,
		EnqueuedAt:   now,
		AvailableAt:  now,
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != reducer.ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}

	wantLive := cloudjoin.CloudResourceUID("111122223333", "us-east-1", "aws_ec2_vpc", "vpc-live")
	if probe.calls != 1 || len(probe.uids) != 1 || probe.uids[0] != wantLive {
		t.Fatalf("retract candidates = %v (%d calls), want exactly [%q]: the pending gen8 uid must never reach the seam", probe.uids, probe.calls, wantLive)
	}
}
