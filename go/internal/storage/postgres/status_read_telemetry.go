// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
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

// querySummaryKey carries a bounded read name for the Postgres query a caller
// is about to run.
type querySummaryKey struct{}

// withQuerySummary labels ctx with a bounded read name. InstrumentedDB stamps it
// on the postgres.query span as db.query.summary.
func withQuerySummary(ctx context.Context, summary string) context.Context {
	return context.WithValue(ctx, querySummaryKey{}, summary)
}

// querySummaryFromContext returns the read name withQuerySummary set, or "".
func querySummaryFromContext(ctx context.Context) string {
	summary, _ := ctx.Value(querySummaryKey{}).(string)
	return summary
}

// read returns the store's queryer labeled with one status snapshot read, so
// each of the snapshot's statements is attributable in traces and metrics
// (#6794): without it, every status read produced an identical span.
func (s StatusStore) read(label string) db.Queryer {
	return statusReadQueryer{inner: s.queryer, instruments: s.Instruments, read: label}
}

// statusReadQueryer labels every query of one status snapshot read and records
// its duration, from issuing the query until its rows are closed.
type statusReadQueryer struct {
	inner       db.Queryer
	instruments *telemetry.Instruments
	read        string
}

// QueryContext runs the query with the read label on ctx and times it.
func (q statusReadQueryer) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	ctx = withQuerySummary(ctx, q.read)
	start := time.Now()
	rows, err := q.inner.QueryContext(ctx, query, args...)
	if err != nil || rows == nil {
		q.record(ctx, start, err != nil)
		return rows, err
	}
	return &statusReadRows{Rows: rows, done: func(failed bool) { q.record(ctx, start, failed) }}, nil
}

// record adds one sample to eshu_dp_status_snapshot_read_duration_seconds.
func (q statusReadQueryer) record(ctx context.Context, start time.Time, failed bool) {
	if q.instruments == nil || q.instruments.StatusSnapshotReadDuration == nil {
		return
	}
	outcome := statusReadOutcomeSuccess
	if failed {
		outcome = statusReadOutcomeError
	}
	q.instruments.StatusSnapshotReadDuration.Record(ctx, time.Since(start).Seconds(),
		metric.WithAttributes(attribute.String("read", q.read), attribute.String("outcome", outcome)))
}

// statusReadRows records the read's duration once, when its rows close.
type statusReadRows struct {
	db.Rows
	done func(failed bool)
	once sync.Once
}

// Close closes the rows and records the read's duration and outcome.
func (r *statusReadRows) Close() error {
	err := r.Rows.Close()
	r.once.Do(func() { r.done(err != nil || r.Err() != nil) })
	return err
}
