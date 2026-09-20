// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build perf6785_wait

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/iamcan"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/iamcantargets"
)

// R2-F3 cost harness (#6785): per-evaluation wall time of a CAN_PERFORM
// intent that waits on missing cross-scope targets, against real Postgres,
// the real FactStore, and the real iamcantargets store. The graph writer is a
// no-op, so graph time is excluded on both sides. configureWaitHandler
// (readiness_wait_cost_after_perf_test.go at HEAD, a no-op copy at the
// pre-ledger commit) is the only difference between the two runs.
//
// Run with:
//
//	ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	  go test -tags perf6785_wait ./internal/storage/postgres -run ReadinessWaitCost -count=1 -v

const (
	costAccount       = "210987654321"
	costRoles         = 1000
	costPermissions   = 3000
	costBuckets       = 1000
	costMissing       = 10
	costOtherRegions  = 17
	costOtherServices = 20
	costEvaluations   = 60 // 30 min bound / 30 s RetryDelay
)

type costNoopWriter struct{ rows int }

func (w *costNoopWriter) WriteIAMCanPerformEdges(_ context.Context, rows []map[string]any, _, _, _ string) error {
	w.rows = len(rows)
	return nil
}

func (*costNoopWriter) RetractIAMCanPerformEdges(context.Context, []string, string, string) error {
	return nil
}

func TestReadinessWaitCostPerf(t *testing.T) {
	sqlDB, _ := awsCloudRuntimeDriftAdmissionLiveDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := sqlDB.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, query)
		}
	}
	run := os.Getenv("ESHU_PERF6785_RUN")
	if run == "" {
		run = fmt.Sprint(time.Now().UnixNano())
	}
	iamScope := fmt.Sprintf("aws:%s:us-east-1:iam", costAccount)
	iamGen := "cost-iam-" + run
	eastS3, westS3 := fmt.Sprintf("aws:%s:us-east-1:s3", costAccount), fmt.Sprintf("aws:%s:us-west-2:s3", costAccount)
	now := time.Now().UTC()
	exec(`DELETE FROM ingestion_scopes WHERE scope_id LIKE 'aws:' || $1 || ':%'`, costAccount)
	exec(`DO $clear$ BEGIN
		IF to_regclass('reducer_readiness_waits') IS NOT NULL THEN
			DELETE FROM reducer_readiness_waits WHERE scope_id LIKE 'aws:210987654321:%';
		END IF;
	END $clear$`)

	seedScope := func(scopeID, generationID string) {
		exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
			partition_key, observed_at, ingested_at, status, active_generation_id, payload)
			VALUES ($1, 'aws', 'aws', $1, 'aws', $1, $2, $2, 'active', NULL, '{}'::jsonb)`, scopeID, now)
		exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
			VALUES ($1, $2, 'manual', $3, $3, 'active', $3)`, generationID, scopeID, now)
		exec(`UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scopeID, generationID)
	}
	// Account background: every other region/service scope is registered.
	for region := 0; region < costOtherRegions; region++ {
		for service := 0; service < costOtherServices; service++ {
			scopeID := fmt.Sprintf("aws:%s:bg-region-%02d:svc%02d", costAccount, region, service)
			seedScope(scopeID, fmt.Sprintf("cost-bg-%d-%d-%s", region, service, run))
		}
	}
	seedScope(iamScope, iamGen)
	seedScope(eastS3, "cost-s3e-"+run)
	seedScope(westS3, "cost-s3w-"+run)
	exec(`INSERT INTO graph_projection_phase_state (scope_id, acceptance_unit_id, source_run_id, generation_id,
		keyspace, phase, committed_at, updated_at)
		VALUES ($1, 'aws_resource_materialization:' || $1, $2, $2, 'cloud_resource_uid', 'canonical_nodes_committed', $3, $3)`,
		eastS3, "cost-s3e-"+run, now)

	insertFact := func(factID, scopeID, generationID, kind string, payload map[string]any) {
		raw, _ := json.Marshal(payload)
		exec(`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
			collector_kind, source_confidence, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
			VALUES ($1, $2, $3, $4, $1, '1.0.0', 'aws', 'observed', 'aws', $1, $5, $5, false, $6::jsonb)`,
			factID, scopeID, generationID, kind, now, string(raw))
	}
	bucketARN := func(i int) string { return fmt.Sprintf("arn:aws:s3:::cost-bucket-%04d", i) }
	roleARN := func(i int) string { return fmt.Sprintf("arn:aws:iam::%s:role/cost-role-%03d", costAccount, i) }
	for i := 0; i < costRoles; i++ {
		insertFact(fmt.Sprintf("%s-role-%d", run, i), iamScope, iamGen, "aws_resource", map[string]any{
			"account_id": costAccount, "region": "us-east-1", "resource_type": "aws_iam_role",
			"resource_id": roleARN(i), "arn": roleARN(i), "correlation_anchors": []any{},
		})
	}
	// Each role gets one exact s3 grant on its own bucket plus two uncatalogued
	// statements, so the 3000 permission facts resolve to at most 1000 edges.
	for i := 0; i < costPermissions; i++ {
		action, resource := "s3:getobject", bucketARN(i%costBuckets)
		if i >= costRoles {
			action, resource = "sqs:sendmessage", fmt.Sprintf("arn:aws:sqs:us-east-1:%s:cost-queue-%d", costAccount, i)
		}
		insertFact(fmt.Sprintf("%s-perm-%d", run, i), iamScope, iamGen, "aws_iam_permission", map[string]any{
			"account_id": costAccount, "region": "us-east-1", "principal_arn": roleARN(i % costRoles),
			"principal_type": "role", "policy_source": "inline", "effect": "Allow",
			"actions": []any{action}, "not_actions": []any{}, "resources": []any{resource},
			"not_resources": []any{}, "condition_keys": []any{}, "assume_principals": []any{},
			"has_conditions": false, "is_wildcard_action": false, "is_wildcard_resource": false,
		})
	}
	for i := 0; i < costBuckets; i++ {
		scopeID, generationID := eastS3, "cost-s3e-"+run
		if i < costMissing {
			scopeID, generationID = westS3, "cost-s3w-"+run // nodes never commit: not_ready
		}
		insertFact(fmt.Sprintf("%s-bucket-%d", run, i), scopeID, generationID, "aws_resource", map[string]any{
			"account_id": costAccount, "region": "us-east-1", "resource_type": "aws_s3_bucket",
			"resource_id": bucketARN(i), "arn": bucketARN(i), "correlation_anchors": []any{},
		})
	}
	exec(`ANALYZE fact_records`)
	exec(`ANALYZE ingestion_scopes`)

	factStore := NewFactStore(SQLDB{DB: sqlDB})
	writer := &costNoopWriter{}
	clock := now
	handler := iamcan.IAMCanPerformMaterializationHandler{
		FactLoader:        factStore,
		Writer:            writer,
		CrossScopeTargets: iamcantargets.Store{DB: SQLDB{DB: sqlDB}, Facts: factStore},
	}
	mode := configureWaitHandler(&handler, sqlDB, func() time.Time { return clock })
	intent := reducer.Intent{
		IntentID: "cost-" + run, ScopeID: iamScope, GenerationID: iamGen,
		Domain:     reducer.DomainIAMCanPerformMaterialization,
		EntityKeys: []string{"aws_resource_materialization:" + iamScope},
		EnqueuedAt: now, AvailableAt: now, CycleStartedAt: now, AttemptCount: 1,
	}

	durations := make([]time.Duration, 0, costEvaluations)
	deferred := 0
	for i := 0; i < costEvaluations; i++ {
		start := time.Now()
		_, err := handler.Handle(ctx, intent)
		durations = append(durations, time.Since(start))
		if err != nil {
			deferred++
		}
		clock = clock.Add(30 * time.Second)
	}
	report(t, mode, "first evaluation", durations[:1])
	report(t, mode, "evaluations 2..60", durations[1:])
	var total time.Duration
	for _, d := range durations {
		total += d
	}
	t.Logf("%s: %d evaluations in one generation, %d returned the not-ready error, edges on last commit=%d, summed handler time %.1f ms",
		mode, len(durations), deferred, writer.rows, float64(total.Microseconds())/1000)

	// Next generation with the same missing set: a new queue cycle.
	clock = clock.Add(time.Minute)
	intent.CycleStartedAt = clock
	start := time.Now()
	_, err := handler.Handle(ctx, intent)
	t.Logf("%s: next generation, first claim: %.2f ms, not-ready=%v", mode, float64(time.Since(start).Microseconds())/1000, err != nil)
}

func report(t *testing.T, mode, label string, durations []time.Duration) {
	t.Helper()
	sorted := append([]time.Duration(nil), durations...)
	sort.Slice(sorted, func(a, b int) bool { return sorted[a] < sorted[b] })
	var sum time.Duration
	for _, d := range sorted {
		sum += d
	}
	ms := func(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }
	t.Logf("%s: %s n=%d mean=%.2f ms p50=%.2f ms p95=%.2f ms max=%.2f ms", mode, label, len(sorted),
		ms(sum/time.Duration(len(sorted))), ms(sorted[len(sorted)/2]), ms(sorted[len(sorted)*95/100]), ms(sorted[len(sorted)-1]))
}
