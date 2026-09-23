// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Status snapshot read labels: the closed value set of the `read` attribute on
// eshu_dp_status_snapshot_read_duration_seconds and of db.query.summary on the
// postgres.query span. One label per reader in ReadStatusSnapshotFiltered.
const (
	statusReadScopeCounts                    = "scope_counts"
	statusReadGenerationCounts               = "generation_counts"
	statusReadGenerationTransitions          = "generation_transitions"
	statusReadActiveWorkSummary              = "active_work_summary"
	statusReadProducerActivity               = "producer_activity"
	statusReadCollectorGenerationDeadLetters = "collector_generation_dead_letters"
	statusReadCoordinator                    = "coordinator"
	statusReadRegistryCollectors             = "registry_collectors"
	statusReadAWSCloudScans                  = "aws_cloud_scans"
	statusReadAWSFreshness                   = "aws_freshness"
	statusReadInfraInventory                 = "infra_inventory"
	statusReadVulnerabilitySources           = "vulnerability_sources"
	statusReadCollectorFactEvidence          = "collector_fact_evidence"
	statusReadTerraformState                 = "terraform_state"
	statusReadSemanticExtraction             = "semantic_extraction"
)

// Bounded outcome values for eshu_dp_status_snapshot_read_duration_seconds.
const (
	statusReadOutcomeSuccess = "success"
	statusReadOutcomeError   = "error"
)

// read starts one labeled status snapshot read (#6794). It returns the store's
// queryer labeled with the read, so each statement is attributable on the
// postgres.query span, and a done func the caller passes the reader's returned
// error to. done records one duration sample per read, from start until the
// reader returns, with outcome=error for any reader failure: query, iteration,
// scan conversion, or post-scan decode. It returns err unchanged.
func (s StatusStore) read(ctx context.Context, label string) (db.Queryer, func(error) error) {
	start := time.Now()
	queryer := statusReadQueryer{inner: s.queryer, read: label}
	return queryer, func(err error) error {
		s.recordRead(ctx, label, start, err != nil)
		return err
	}
}

// recordRead adds one sample to eshu_dp_status_snapshot_read_duration_seconds.
func (s StatusStore) recordRead(ctx context.Context, label string, start time.Time, failed bool) {
	if s.Instruments == nil || s.Instruments.StatusSnapshotReadDuration == nil {
		return
	}
	outcome := statusReadOutcomeSuccess
	if failed {
		outcome = statusReadOutcomeError
	}
	s.Instruments.StatusSnapshotReadDuration.Record(ctx, time.Since(start).Seconds(),
		metric.WithAttributes(telemetry.AttrRead(label), telemetry.AttrOutcome(outcome)))
}

// statusReadQueryer labels every query of one status snapshot read.
type statusReadQueryer struct {
	inner db.Queryer
	read  string
}

// QueryContext runs the query with the read label on ctx.
func (q statusReadQueryer) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return q.inner.QueryContext(db.WithQuerySummary(ctx, q.read), query, args...)
}
