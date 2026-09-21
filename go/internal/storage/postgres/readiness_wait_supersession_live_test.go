// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/iamcan"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/readiness/wait"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// Run with:
//
//	ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	  go test ./internal/storage/postgres -run ReadinessWaitSurvivesSupersession -count=1 -v

const supersessionAccount = "123456789012"

// supersessionFacts is one role with two exact s3 grants in the iam scope.
type supersessionFacts struct{ envelopes []facts.Envelope }

func (f supersessionFacts) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	return f.envelopes, nil
}

// supersessionTargets answers with the ready bucket committed and the other
// bucket in an s3 scope whose nodes never commit.
type supersessionTargets struct{ ready, missing string }

func (s supersessionTargets) LoadCrossScopeTargets(context.Context, iamcan.CrossScopeTargetRequest) (iamcan.CrossScopeTargetSnapshot, error) {
	bucket := func(arn string) facts.Envelope {
		return facts.Envelope{FactKind: facts.AWSResourceFactKind, Payload: map[string]any{
			"account_id": supersessionAccount, "region": "us-east-1", "resource_type": awsv1.ResourceTypeS3Bucket,
			"resource_id": arn, "arn": arn, "correlation_anchors": []any{},
		}}
	}
	return iamcan.CrossScopeTargetSnapshot{
		Scopes: []iamcan.CrossScopeTargetScope{
			{ScopeID: "aws:" + supersessionAccount + ":us-east-1:s3", ActiveGenerationID: "s3-a", GenerationActive: true, NodesCommitted: true},
			{ScopeID: "aws:" + supersessionAccount + ":us-west-2:s3", ActiveGenerationID: "s3-b", GenerationActive: true},
		},
		Resources: map[string][]facts.Envelope{
			"aws:" + supersessionAccount + ":us-east-1:s3": {bucket(s.ready)},
			"aws:" + supersessionAccount + ":us-west-2:s3": {bucket(s.missing)},
		},
	}, nil
}

type supersessionWriter struct{ writes, retracts, rows int }

func (w *supersessionWriter) WriteIAMCanPerformEdges(_ context.Context, rows []map[string]any, _, _, _ string) error {
	w.writes++
	w.rows += len(rows)
	return nil
}

func (w *supersessionWriter) RetractIAMCanPerformEdges(context.Context, []string, string, string) error {
	w.retracts++
	return nil
}

func supersessionPermission(roleARN, action, resource string) facts.Envelope {
	return facts.Envelope{FactKind: facts.AWSIAMPermissionFactKind, Payload: map[string]any{
		"account_id": supersessionAccount, "region": "us-east-1", "principal_arn": roleARN,
		"principal_type": "role", "policy_source": "inline", "effect": "Allow",
		"actions": []any{action}, "not_actions": []any{}, "resources": []any{resource},
		"not_resources": []any{}, "condition_keys": []any{}, "assume_principals": []any{},
		"has_conditions": false, "is_wildcard_action": false, "is_wildcard_resource": false,
	}}
}

// TestReadinessWaitSurvivesSupersessionLive is §4.5 item 1, the reviewer's
// R2-F1 closing check against the REAL reducer queue and the REAL ledger:
//
//  1. gen N is claimed; the handler commits the ready edge and defers.
//  2. gen N+1 activates before the bound, with the target still missing. The
//     next Claim supersedes N (terminal) and returns N+1, whose first
//     evaluation commits at once.
//  3. At anchor+MaxWait -- measured from N's first defer, not from N+1's row --
//     the N+1 row settles and is acked, and abandoned is counted exactly once.
//
// Before #6785's ledger the anchor was N+1's own created_at, so step 3 kept
// deferring: the bound restarted with every superseding generation.
func TestReadinessWaitSurvivesSupersessionLive(t *testing.T) {
	sqlDB, ctx := awsCloudRuntimeDriftAdmissionLiveDB(t)
	suffix := time.Now().UnixNano()
	scopeID := fmt.Sprintf("aws:%s:us-east-1:iam-6785-%d", supersessionAccount, suffix)
	genN := fmt.Sprintf("iam-gen-n-%d", suffix)
	genN1 := fmt.Sprintf("iam-gen-n1-%d", suffix)
	// The queue's lease fence compares claim_until with the database's real
	// clock_timestamp(), so the test clock starts at real time and only moves
	// forward.
	start := time.Now().UTC().Truncate(time.Millisecond)
	clock := start
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM fact_work_items WHERE scope_id = $1`,
			`DELETE FROM reducer_readiness_waits WHERE scope_id = $1`,
			`DELETE FROM graph_projection_phase_state WHERE scope_id = $1`,
			`DELETE FROM scope_generations WHERE scope_id = $1`,
			`DELETE FROM ingestion_scopes WHERE scope_id = $1`,
		} {
			if _, err := sqlDB.ExecContext(context.Background(), q, scopeID); err != nil {
				t.Errorf("cleanup %q: %v", q, err)
			}
		}
	})

	seedAWSCloudRuntimeDriftScope(t, ctx, sqlDB, scopeID, "aws", genN, clock)
	seedAWSCloudRuntimeDriftGeneration(t, ctx, sqlDB, genN, scopeID, "active", clock)
	seedCloudNodesCommitted(t, ctx, sqlDB, scopeID, genN, clock)

	queue := ReducerQueue{
		database: SQLDB{DB: sqlDB}, LeaseOwner: "reducer-6785-supersession", LeaseDuration: time.Minute,
		RetryDelay: 30 * time.Second, MaxAttempts: 3, Now: func() time.Time { return clock },
		// Claim is queue-global; restricting the domain keeps rows other live
		// tests leave on a shared database out of this proof.
		ClaimDomain: reducer.DomainIAMCanPerformMaterialization,
	}
	enqueue := func(generation string) {
		t.Helper()
		if _, err := queue.Enqueue(ctx, []runtime.ReducerIntent{{
			ScopeID: scopeID, GenerationID: generation, Domain: reducer.DomainIAMCanPerformMaterialization,
			EntityKey: "aws_resource_materialization:" + scopeID, Reason: "iam permissions observed", SourceSystem: "aws",
		}}); err != nil {
			t.Fatalf("Enqueue(%s): %v", generation, err)
		}
	}
	claim := func(wantGeneration string) reducer.Intent {
		t.Helper()
		intent, ok, err := queue.Claim(ctx)
		if err != nil || !ok {
			t.Fatalf("Claim() = ok %v err %v, want %s", ok, err, wantGeneration)
		}
		if intent.GenerationID != wantGeneration {
			t.Fatalf("claimed generation %s, want %s", intent.GenerationID, wantGeneration)
		}
		return intent
	}

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	roleARN := "arn:aws:iam::" + supersessionAccount + ":role/app-role"
	readyBucket, missingBucket := "arn:aws:s3:::ready-6785", "arn:aws:s3:::missing-6785"
	writer := &supersessionWriter{}
	handler := iamcan.IAMCanPerformMaterializationHandler{
		FactLoader: supersessionFacts{envelopes: []facts.Envelope{
			{FactKind: facts.AWSResourceFactKind, Payload: map[string]any{
				"account_id": supersessionAccount, "region": "us-east-1", "resource_type": awsv1.ResourceTypeIAMRole,
				"resource_id": roleARN, "arn": roleARN, "correlation_anchors": []any{},
			}},
			supersessionPermission(roleARN, "s3:putobject", readyBucket),
			supersessionPermission(roleARN, "s3:getobject", missingBucket),
		}},
		Writer:            writer,
		CrossScopeTargets: supersessionTargets{ready: readyBucket, missing: missingBucket},
		ReadinessWaits:    wait.Store{DB: SQLDB{DB: sqlDB}},
		ReadinessMaxWait:  10 * time.Minute,
		Now:               func() time.Time { return clock },
		Instruments:       instruments,
	}
	handle := func(intent reducer.Intent) error {
		t.Helper()
		_, handleErr := handler.Handle(ctx, intent)
		if handleErr == nil {
			if err := queue.Ack(ctx, intent, reducer.Result{}); err != nil {
				t.Fatalf("Ack: %v", err)
			}
			return nil
		}
		var classified interface{ FailureClass() string }
		if !errors.As(handleErr, &classified) || classified.FailureClass() != iamcan.IAMCanPerformTargetNotReadyFailureClass {
			t.Fatalf("Handle() error = %v, want the not-ready defer", handleErr)
		}
		if err := queue.Fail(ctx, intent, handleErr); err != nil {
			t.Fatalf("Fail: %v", err)
		}
		return handleErr
	}

	// Step 1: gen N commits the ready edge, then defers.
	enqueue(genN)
	if err := handle(claim(genN)); err == nil {
		t.Fatal("gen N first evaluation succeeded, want commit then defer")
	}
	if writer.writes != 1 || writer.rows != 1 {
		t.Fatalf("gen N: writes %d rows %d, want the ready edge written at the first evaluation", writer.writes, writer.rows)
	}

	// Step 2: gen N+1 activates before the bound; the claim supersedes N.
	clock = start.Add(6 * time.Minute)
	// Activation retires the prior generation, as the projector's does.
	seedAWSCloudRuntimeDriftGeneration(t, ctx, sqlDB, genN, scopeID, "superseded", start)
	seedAWSCloudRuntimeDriftGeneration(t, ctx, sqlDB, genN1, scopeID, "active", clock)
	seedAWSCloudRuntimeDriftScope(t, ctx, sqlDB, scopeID, "aws", genN1, clock)
	seedCloudNodesCommitted(t, ctx, sqlDB, scopeID, genN1, clock)
	enqueue(genN1)
	gen1Claim := claim(genN1)
	if status := reducerWorkStatus(t, ctx, sqlDB, scopeID, genN); status != "superseded" {
		t.Fatalf("gen N row status = %q, want superseded", status)
	}
	if err := handle(gen1Claim); err == nil {
		t.Fatal("gen N+1 first evaluation succeeded, want commit then defer")
	}
	if writer.writes != 2 {
		t.Fatalf("gen N+1 first evaluation writes = %d total, want a commit at its first claim", writer.writes)
	}

	// Step 3: at N's anchor + MaxWait the N+1 row settles and is acked.
	clock = start.Add(10 * time.Minute)
	if err := handle(claim(genN1)); err != nil {
		t.Fatalf("gen N+1 at anchor+MaxWait error = %v, want the wait to settle", err)
	}
	if status := reducerWorkStatus(t, ctx, sqlDB, scopeID, genN1); status != "succeeded" {
		t.Fatalf("gen N+1 row status = %q, want succeeded", status)
	}
	if got := readinessWaitOutcomeTotal(t, reader, "abandoned"); got != 1 {
		t.Fatalf("eshu_dp_reducer_readiness_waits_total{outcome=abandoned} = %d, want 1", got)
	}
	t.Logf("supersession: N superseded at +6m, N+1 committed at first claim, settled at +10m (anchor from N); writes=%d retracts=%d",
		writer.writes, writer.retracts)
}

func seedCloudNodesCommitted(t *testing.T, ctx context.Context, db *sql.DB, scopeID, generationID string, now time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO graph_projection_phase_state
		  (scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase, committed_at, updated_at)
		VALUES ($1, 'aws_resource_materialization:' || $1, $2, $2, 'cloud_resource_uid', 'canonical_nodes_committed', $3, $3)
		ON CONFLICT DO NOTHING`, scopeID, generationID, now); err != nil {
		t.Fatalf("seed phase state %s/%s: %v", scopeID, generationID, err)
	}
}

func reducerWorkStatus(t *testing.T, ctx context.Context, db *sql.DB, scopeID, generationID string) string {
	t.Helper()
	var status string
	if err := db.QueryRowContext(ctx,
		`SELECT status FROM fact_work_items WHERE scope_id = $1 AND generation_id = $2 AND stage = 'reducer'`,
		scopeID, generationID).Scan(&status); err != nil {
		t.Fatalf("read work status %s: %v", generationID, err)
	}
	return status
}

func readinessWaitOutcomeTotal(t *testing.T, reader *sdkmetric.ManualReader, outcome string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if m.Name != "eshu_dp_reducer_readiness_waits_total" || !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				domain, _ := point.Attributes.Value("domain")
				value, _ := point.Attributes.Value("outcome")
				if domain.AsString() == string(reducercontract.DomainIAMCanPerformMaterialization) && value.AsString() == outcome {
					total += point.Value
				}
			}
		}
	}
	return total
}
