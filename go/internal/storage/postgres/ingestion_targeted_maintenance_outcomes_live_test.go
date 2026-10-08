// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestTargetedMaintenanceOutcomesMatchWholePass covers the #7584 follow-up
// fixtures: per-owed outcomes decided before the catalog guard, the
// no-memo-baseline refusal, the NULL active pointer left by the real projector
// Fail path, an owed repo_id DISTINCT ON collision loser, and the no-anchor
// (empty catalog) path. Every differential case asserts the same 0/0 equality
// as TestTargetedMaintenanceMatchesWholePass.
func TestTargetedMaintenanceOutcomesMatchWholePass(t *testing.T) {
	t.Run("superseded_owed_reports_not_active_under_catalog_change", func(t *testing.T) {
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		p.prepass()
		p.quietGeneration("git:tgt", "tgt-2", "repo-tgt", "payments-service", "orders-api")
		p.exec(`UPDATE scope_generations SET ingested_at = ingested_at - interval '30 minutes' WHERE generation_id = 'tgt-2'`)
		p.quietGeneration("git:tgt", "tgt-3", "repo-tgt", "payments-service", "orders-api")
		p.gitRepo("git:new", "new-1", "repo-new", "inventory-svc")
		before := p.capture(p.targeted)
		result, err := targetedDiffStore(p.targeted, targetedDiffArmsAt).RunDeferredRelationshipMaintenanceForPartitions(
			p.ctx, nil, nil, owedPartitions("git:tgt", "tgt-2"))
		if err != nil {
			t.Fatalf("superseded owed under a catalog change: error = %v, want nil (not_active, not a refusal)", err)
		}
		want := []TargetedMaintenanceOutcome{{Partition: OwedPartition{ScopeID: "git:tgt", GenerationID: "tgt-2"}, Kind: TargetedMaintenanceNotActive}}
		if !reflect.DeepEqual(result.Outcomes, want) {
			t.Fatalf("outcomes = %v, want %v", result.Outcomes, want)
		}
		if changed := diffStates(before, before, p.capture(p.targeted), nil).targetedChangedOutside; len(changed) > 0 {
			t.Fatalf("not-active pass changed rows: %v", changed)
		}
	})

	t.Run("no_memo_baseline_refuses_until_a_whole_pass", func(t *testing.T) {
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		p.quietGeneration("git:tgt", "tgt-2", "repo-tgt", "payments-service", "orders-api")
		store := targetedDiffStore(p.targeted, targetedDiffArmsAt)
		before := p.capture(p.targeted)
		result, err := store.RunDeferredRelationshipMaintenanceForPartitions(p.ctx, nil, nil, owedPartitions("git:tgt", "tgt-2"))
		if !errors.Is(err, ErrTargetedMaintenanceNoMemoBaseline) {
			t.Fatalf("pass with no active memo row: error = %v, want ErrTargetedMaintenanceNoMemoBaseline", err)
		}
		if result.Outcomes[0].Kind != TargetedMaintenanceRetry {
			t.Fatalf("refused owed outcome = %v, want retry", result.Outcomes)
		}
		if changed := diffStates(before, before, p.capture(p.targeted), nil).targetedChangedOutside; len(changed) > 0 {
			t.Fatalf("refused pass changed rows: %v", changed)
		}
		// The epoch whole pass writes the memo baseline; the targeted pass is
		// then admitted.
		if err := store.RunDeferredRelationshipMaintenance(p.ctx, nil, nil); err != nil {
			t.Fatalf("whole pass: %v", err)
		}
		result, err = store.RunDeferredRelationshipMaintenanceForPartitions(p.ctx, nil, nil, owedPartitions("git:tgt", "tgt-2"))
		if err != nil || result.Outcomes[0].Kind != TargetedMaintenancePublished {
			t.Fatalf("pass after the baseline: outcomes=%v err=%v, want published", result.Outcomes, err)
		}
	})

	t.Run("null_active_pointer_after_projector_fail", func(t *testing.T) {
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		p.prepass()
		p.quietGeneration("git:tgt", "tgt-2", "repo-tgt", "payments-service", "orders-api")
		p.exec(`INSERT INTO fact_work_items
  (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, lease_owner, claim_until, visible_at, created_at, updated_at)
VALUES ('tgt-2/projector', 'git:tgt', 'tgt-2', 'projector', 'source_local', 'running', 1, 'proof-worker', $1, $1, $1, $1)`,
			targetedDiffBase.Add(3*time.Hour))
		for _, database := range []*sql.DB{p.whole, p.targeted} {
			failGenerationThroughProjector(t, p.ctx, database, "git:tgt", "tgt-2")
		}
		p.run("null_active_pointer_after_projector_fail", targetedDiffCase{
			owed:        owedPartitions("git:tgt", "tgt-2"),
			compared:    partitionSet("git:tgt", "tgt-2"),
			newEvidence: []string{"repo-tgt->repo-dep"},
			// Both passes resolve the scope through COALESCE(pointer, latest),
			// so both publish for the failed latest generation (the
			// consumer's finalize reads the raw pointer and retires it).
			published: partitionSet("git:tgt", "tgt-2"),
			// Every floored listing excludes failed generations in both arms:
			// the correlation listing always did, and #7637 extends the same
			// floor to deployment_mapping and code_import_repo_edge. tgt-2
			// failed through the real projector Fail above, so nothing reopens.
			reopened: nil,
		})
	})

	t.Run("owed_repo_id_collision_loser", func(t *testing.T) {
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		p.gitRepo("git:b", "b-1", "shared", "beta-svc")
		p.workItems("git:b", "b-1")
		p.prepass()
		// git:a derives the same repo_id with an identical payload, so the
		// catalog is unchanged; DISTINCT ON keeps git:b (fact_id DESC).
		p.scope("git:a")
		p.generation("git:a", "a-1", 90*time.Minute, true)
		p.repo("git:a", "a-1", "shared", "beta-svc")
		p.terraformRef("a-1-ref", "git:a", "a-1", "shared", "a.tf", "orders-api")
		p.workItems("git:a", "a-1")
		p.run("owed_repo_id_collision_loser", targetedDiffCase{
			owed:     owedPartitions("git:a", "a-1"),
			compared: partitionSet("git:b", "b-1"),
			// The new shared->repo-dep evidence (from a-1's terraform fact)
			// lands under b-1's generation: the catalog keeps git:b for the
			// collided repo_id. b-1's own facts are unchanged, so it is a
			// memo hit — but the inbound evidence is genuinely new, so both
			// arms reopen its relationship items too (issue #7636: the
			// skip set is revised with the rows actually inserted).
			newEvidence: []string{"shared->repo-dep"},
			published:   partitionSet("git:b", "b-1"),
			reopened:    workIDs("b-1"),
			outcomes:    map[string]TargetedMaintenanceOutcomeKind{"git:a/a-1": TargetedMaintenanceInapplicable},
		})
	})

	t.Run("no_anchor_empty_catalog", func(t *testing.T) {
		p := newTargetedDiffPair(t)
		p.scope(targetedGCPScope)
		p.generation(targetedGCPScope, "gcp-1", 0, true)
		p.gcpRelation("gcp-1-edge", "gcp-1", "order-gateway", "payments-service")
		p.workItems(targetedGCPScope, "gcp-1")
		p.run("no_anchor_empty_catalog", targetedDiffCase{
			owed:     owedPartitions(targetedGCPScope, "gcp-1"),
			compared: map[scopeGenerationPartition]struct{}{},
			outcomes: map[string]TargetedMaintenanceOutcomeKind{targetedGCPScope + "/gcp-1": TargetedMaintenanceInapplicable},
		})
	})

	t.Run("all_inapplicable_under_refusal_signals_suppression", func(t *testing.T) {
		// Review N1: every owed partition is inapplicable while the catalog
		// guard would refuse. The pass still returns nil (no phase is owed),
		// but it must say that its refusal suppressed the evidence work.
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		p.prepass()
		p.scope(targetedGCPScope)
		p.generation(targetedGCPScope, "gcp-1", 0, true)
		p.gcpRelation("gcp-1-edge", "gcp-1", "order-gateway", "payments-service")
		p.workItems(targetedGCPScope, "gcp-1")
		p.exec(`INSERT INTO deferred_backfill_partition_memo (scope_id, generation_id, catalog_fingerprint, committed_at)
VALUES ('git:dep', 'dep-1', 'stale-fingerprint', $1) ON CONFLICT (scope_id, generation_id) DO UPDATE SET catalog_fingerprint = EXCLUDED.catalog_fingerprint`, targetedDiffBase)
		reader := sdkmetric.NewManualReader()
		instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("tgt7584-suppressed"))
		if err != nil {
			t.Fatalf("NewInstruments: %v", err)
		}
		result, err := targetedDiffStore(p.targeted, targetedDiffArmsAt).RunDeferredRelationshipMaintenanceForPartitions(
			p.ctx, nil, instruments, owedPartitions(targetedGCPScope, "gcp-1"))
		if err != nil {
			t.Fatalf("all-inapplicable pass: error = %v, want nil", err)
		}
		if result.SuppressedRefusal != "catalog_changed" {
			t.Fatalf("SuppressedRefusal = %q, want catalog_changed", result.SuppressedRefusal)
		}
		var rm metricdata.ResourceMetrics
		if err := reader.Collect(context.Background(), &rm); err != nil {
			t.Fatalf("collect: %v", err)
		}
		if got := targetedHistogramCountByOutcome(rm, "eshu_dp_deferred_backfill_targeted_duration_seconds", "suppressed"); got != 1 {
			t.Fatalf("duration{outcome=suppressed} samples = %d, want 1", got)
		}
		if got := targetedCounter(rm, "eshu_dp_deferred_backfill_targeted_outcomes_total", "outcome", "inapplicable"); got != 1 {
			t.Fatalf("outcomes_total{outcome=inapplicable} = %d, want 1", got)
		}
	})

	t.Run("telemetry_counts_targeted_only", func(t *testing.T) {
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		p.prepass()
		p.quietGeneration("git:tgt", "tgt-2", "repo-tgt", "payments-service", "orders-api")
		reader := sdkmetric.NewManualReader()
		instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("tgt7584"))
		if err != nil {
			t.Fatalf("NewInstruments: %v", err)
		}
		store := targetedDiffStore(p.targeted, targetedDiffArmsAt)
		if _, err := store.RunDeferredRelationshipMaintenanceForPartitions(p.ctx, nil, instruments, owedPartitions("git:tgt", "tgt-2")); err != nil {
			t.Fatalf("targeted pass: %v", err)
		}
		p.exec(`INSERT INTO deferred_backfill_partition_memo (scope_id, generation_id, catalog_fingerprint, committed_at)
VALUES ('git:dep', 'dep-1', 'stale-fingerprint', $1) ON CONFLICT (scope_id, generation_id) DO UPDATE SET catalog_fingerprint = EXCLUDED.catalog_fingerprint`, targetedDiffBase)
		if _, err := store.RunDeferredRelationshipMaintenanceForPartitions(p.ctx, nil, instruments, owedPartitions("git:tgt", "tgt-2")); !errors.Is(err, ErrTargetedMaintenanceCatalogChanged) {
			t.Fatalf("stale memo: error = %v, want ErrTargetedMaintenanceCatalogChanged", err)
		}
		var rm metricdata.ResourceMetrics
		if err := reader.Collect(context.Background(), &rm); err != nil {
			t.Fatalf("collect: %v", err)
		}
		// outcomes_total counts owed partitions only: the refused
		// pass adds no pass-level sample and does not count its held owed
		// partition as retry. Pass outcomes live on the duration histogram.
		for outcome, want := range map[string]int64{"published": 1, "catalog_changed": 0, "retry": 0} {
			if got := targetedCounter(rm, "eshu_dp_deferred_backfill_targeted_outcomes_total", "outcome", outcome); got != want {
				t.Fatalf("outcomes_total{outcome=%s} = %d, want %d", outcome, got, want)
			}
		}
		for outcome, want := range map[string]uint64{"completed": 1, "catalog_changed": 1} {
			if got := targetedHistogramCountByOutcome(rm, "eshu_dp_deferred_backfill_targeted_duration_seconds", outcome); got != want {
				t.Fatalf("duration{outcome=%s} samples = %d, want %d", outcome, got, want)
			}
		}
		if got := targetedCounter(rm, "eshu_dp_deferred_backfill_targeted_reopened_total", "domain", "deployment_mapping"); got != 1 {
			t.Fatalf("reopened_total{domain=deployment_mapping} = %d, want 1", got)
		}
		if got := targetedHistogramCount(rm, "eshu_dp_deferred_backfill_targeted_duration_seconds"); got != 2 {
			t.Fatalf("duration samples = %d, want 2", got)
		}
		// The whole pass's series must not move when only the targeted pass ran.
		for _, name := range []string{
			"eshu_dp_deferred_backfill_partitions_total",
			"eshu_dp_deferred_backfill_partitions_skipped_total",
			"eshu_dp_deferred_backfill_partitions_loaded_total",
			"eshu_dp_deferred_backfill_batches_completed_total",
			"eshu_dp_deferred_backfill_fanin_published_total",
			"eshu_dp_reopen_skipped_by_partition_memo_total",
			"eshu_dp_deployment_mapping_reopened_total",
			"eshu_dp_code_import_repo_edge_reopened_total",
			"eshu_dp_correlation_reopened_total",
		} {
			if got := targetedCounter(rm, name, "", ""); got != 0 {
				t.Fatalf("shared whole-pass series %s moved by %d during a targeted pass", name, got)
			}
		}
	})
}

// failGenerationThroughProjector fails the running projector item of
// (scopeID, generationID) through the real ProjectorQueue.Fail path, which
// nulls the scope's active pointer when the failed generation is the active
// one, and asserts the pointer is NULL afterwards.
func failGenerationThroughProjector(t *testing.T, ctx context.Context, database *sql.DB, scopeID, generationID string) {
	t.Helper()
	queue := NewProjectorQueue(SQLDB{DB: database}, "proof-worker", time.Minute)
	queue.Now = func() time.Time { return targetedDiffBase.Add(100 * time.Minute) }
	work := projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: scopeID},
		Generation:   scope.ScopeGeneration{GenerationID: generationID},
		AttemptCount: 1,
	}
	if err := queue.Fail(ctx, work, errors.New("injected terminal projection failure")); err != nil {
		t.Fatalf("ProjectorQueue.Fail: %v", err)
	}
	var pointer sql.NullString
	if err := database.QueryRowContext(ctx,
		"SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $1", scopeID).Scan(&pointer); err != nil {
		t.Fatalf("read pointer: %v", err)
	}
	if pointer.Valid {
		t.Fatalf("active pointer after projector Fail = %q, want NULL", pointer.String)
	}
}

// targetedCounter sums an int64 counter's data points, optionally only those
// carrying key=value. A metric with no data points reads as 0.
func targetedCounter(rm metricdata.ResourceMetrics, name, key, value string) int64 {
	var total int64
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				if key != "" {
					if v, ok := dp.Attributes.Value(attribute.Key(key)); !ok || v.AsString() != value {
						continue
					}
				}
				total += dp.Value
			}
		}
	}
	return total
}

// targetedHistogramCount counts a float64 histogram's samples.
// targetedHistogramCountByOutcome counts histogram samples with outcome.
func targetedHistogramCountByOutcome(rm metricdata.ResourceMetrics, name, outcome string) uint64 {
	var total uint64
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != name {
				continue
			}
			if histogram, ok := m.Data.(metricdata.Histogram[float64]); ok {
				for _, dp := range histogram.DataPoints {
					if value, ok := dp.Attributes.Value("outcome"); ok && value.AsString() == outcome {
						total += dp.Count
					}
				}
			}
		}
	}
	return total
}

func targetedHistogramCount(rm metricdata.ResourceMetrics, name string) uint64 {
	var total uint64
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != name {
				continue
			}
			if histogram, ok := m.Data.(metricdata.Histogram[float64]); ok {
				for _, dp := range histogram.DataPoints {
					total += dp.Count
				}
			}
		}
	}
	return total
}
