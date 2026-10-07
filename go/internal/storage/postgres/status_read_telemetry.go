// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"slices"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
	statestore "github.com/eshu-hq/eshu/go/internal/storage/postgres/terraform/state"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Status snapshot read labels: the closed value set of the `read` attribute on
// eshu_dp_status_snapshot_read_duration_seconds and of db.query.summary on the
// postgres.query span. One label per reader in ReadStatusSnapshotFiltered.
// active_work_summary_model labels the stored-summary row read (#7009), so a
// before and after of the reader flag is separable from the live statement.
const (
	statusReadScopeCounts                    = "scope_counts"
	statusReadGenerationCounts               = "generation_counts"
	statusReadGenerationTransitions          = "generation_transitions"
	statusReadActiveWorkSummary              = "active_work_summary"
	statusReadActiveWorkSummaryModel         = "active_work_summary_model"
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
	statusReadTerraformStateModel            = "terraform_state_model"
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

// Span attributes recording which branch the active-work summary gate took
// (#7009 S5 ruling D5.3): summary_mode is grouped or detail, summary_estimate
// the pg_stats live-share estimate the gate compared with the threshold.
const (
	statusActiveWorkSummaryModeKey     = attribute.Key("status.active_work.summary_mode")
	statusActiveWorkSummaryEstimateKey = attribute.Key("status.active_work.summary_estimate")
)

// recordActiveWorkSummaryMode sets the gate branch and estimate on the span
// active on ctx: the postgres.status_snapshot span when the snapshot reader
// wraps the read. A slow active_work_summary sample on
// eshu_dp_status_snapshot_read_duration_seconds is then attributable to its
// branch. A summary without a mode row records nothing.
func recordActiveWorkSummaryMode(ctx context.Context, summary activeWorkSummary) {
	if summary.Mode == "" {
		return
	}
	trace.SpanFromContext(ctx).SetAttributes(
		statusActiveWorkSummaryModeKey.String(summary.Mode),
		statusActiveWorkSummaryEstimateKey.Float64(summary.Estimate),
	)
}

// StatusSummaryReader is the process-wide reader of the stored status summaries
// (#7009), built once at startup: one summary.ModelReader per model, all under
// the same settings, each with its own shared live statement.
type StatusSummaryReader struct {
	activeWork *summary.ModelReader[activeWorkSummary]
	terraform  *summary.ModelReader[statestore.TerraformStateAdminEvidence]
}

// NewStatusSummaryReader loads ESHU_STATUS_SUMMARY_READ_ENABLED and
// ESHU_STATUS_SUMMARY_STALE_AFTER; an invalid value while on is a startup error.
func NewStatusSummaryReader(getenv func(string) string) (*StatusSummaryReader, error) {
	cfg, err := summary.LoadReadConfig(getenv)
	if err != nil {
		return nil, err
	}
	return NewStatusSummaryReaderWithConfig(cfg), nil
}

// NewStatusSummaryReaderWithConfig builds a reader from explicit settings.
func NewStatusSummaryReaderWithConfig(cfg summary.ReadConfig) *StatusSummaryReader {
	return &StatusSummaryReader{
		activeWork: summary.NewModelReaderWithConfig[activeWorkSummary](cfg),
		terraform:  summary.NewModelReaderWithConfig[statestore.TerraformStateAdminEvidence](cfg),
	}
}

// Waiting reports how many reads have joined the in-flight shared live
// active-work statement (see summary.ModelReader.Waiting).
func (r *StatusSummaryReader) Waiting() int { return r.activeWork.Waiting() }

// TerraformWaiting is Waiting for the shared live Terraform-state statements.
func (r *StatusSummaryReader) TerraformWaiting() int { return r.terraform.Waiting() }

// active and terraformModel return the per-model readers, nil when there is no
// process-wide reader (the stored summary is off).
func (r *StatusSummaryReader) active() *summary.ModelReader[activeWorkSummary] {
	if r == nil {
		return nil
	}
	return r.activeWork
}

func (r *StatusSummaryReader) terraformModel() *summary.ModelReader[statestore.TerraformStateAdminEvidence] {
	if r == nil {
		return nil
	}
	return r.terraform
}

// readActiveWork returns the active-work summary and where it came from: the
// stored row when every fence passes, otherwise the live statement. A
// selection that asks for the stored summary only (the runtime /metrics scrape)
// never reaches the live statement while the reader is on; see readActiveWorkScrape.
func (s StatusStore) readActiveWork(ctx context.Context, asOf time.Time, selection statuspkg.SnapshotSelection) (activeWorkSummary, statuspkg.ActiveWorkSource, error) {
	if s.startupErr != nil {
		return activeWorkSummary{}, statuspkg.ActiveWorkSource{}, s.startupErr
	}
	if selection.StoredActiveWorkOnly && s.summaryReader.active().Enabled() {
		return s.readActiveWorkScrape(ctx)
	}
	result, err := s.summaryReader.active().Read(ctx, asOf, summary.Hooks[activeWorkSummary]{
		Select: func(ctx context.Context) (summary.Selection, error) {
			q, done := s.read(ctx, statusReadActiveWorkSummaryModel)
			selection, err := summary.Select(ctx, q, summary.SelectConfig{
				ModelKey: summary.ModelActiveWorkSummary, SourceSHA256: ActiveWorkSummarySourceSHA256(),
				StaleAfter: s.summaryReader.activeWork.Config.StaleAfter,
			})
			return selection, done(err)
		},
		Decode: decodeActiveWorkEntries,
		Live: func(ctx context.Context) (activeWorkSummary, error) {
			q, done := s.read(ctx, statusReadActiveWorkSummary)
			work, err := readActiveWorkSummary(ctx, q, asOf)
			return work, done(err)
		},
		Clone: activeWorkSummary.clone,
		Observe: func(ctx context.Context, selection summary.Selection) {
			summary.Observe(ctx, s.Instruments, summary.Observation{
				ModelKey: summary.ModelActiveWorkSummary, Source: selection.Source, Reason: selection.Reason,
				AsOf: selection.AsOf, Age: selection.Age, SignedAge: selection.SignedAge,
			})
		},
	})
	// Stale stays false: a row that is too old is never served; Reason names it.
	return result.Value, statuspkg.ActiveWorkSource{
		Source: string(result.Source), Reason: string(result.Reason), AsOf: result.AsOf, Age: result.Age,
	}, err
}

// readActiveWorkScrape answers the scrape path: a fresh stored row, else the
// newest row the process can decode (a stale one included), else the zero
// summary, and never the live statement. The marker carries Stale for the last two and the row's age at
// this read. A database error fails the read, like every other status read.
func (s StatusStore) readActiveWorkScrape(ctx context.Context) (activeWorkSummary, statuspkg.ActiveWorkSource, error) {
	result, err := s.summaryReader.active().ReadScrape(ctx, summary.ScrapeHooks[activeWorkSummary]{
		Select: func(ctx context.Context) (summary.Selection, error) {
			q, done := s.read(ctx, statusReadActiveWorkSummaryModel)
			selection, err := summary.Select(ctx, q, summary.SelectConfig{
				ModelKey: summary.ModelActiveWorkSummary, SourceSHA256: ActiveWorkSummarySourceSHA256(),
				StaleAfter: s.summaryReader.activeWork.Config.StaleAfter, DecodeStale: true,
			})
			return selection, done(err)
		},
		Decode: decodeActiveWorkEntries,
		Observe: func(ctx context.Context, o summary.ScrapeObservation) {
			o.ModelKey = summary.ModelActiveWorkSummary
			summary.ObserveScrape(ctx, s.Instruments, o)
		},
	})
	return result.Value, statuspkg.ActiveWorkSource{
		Source: string(result.Source), Reason: string(result.Reason), AsOf: result.AsOf, Age: result.Age, Stale: result.Stale,
	}, err
}

// decodeActiveWorkEntries decodes stored entries with the live read's decoder.
func decodeActiveWorkEntries(entries []summary.Entry) (activeWorkSummary, error) {
	work := activeWorkSummary{
		StageCounts:    []statuspkg.StageStatusCount{},
		DomainBacklogs: []statuspkg.DomainBacklog{},
		Blockages:      []statuspkg.QueueBlockage{},
	}
	for _, entry := range entries {
		if err := work.add(entry.Section, entry.JSON); err != nil {
			return activeWorkSummary{}, fmt.Errorf("decode stored active work summary %s row %d: %w", entry.Section, entry.Ordinal, err)
		}
	}
	return work, nil
}

// clone returns a copy that shares no slice or pointer with s.
func (s activeWorkSummary) clone() activeWorkSummary {
	s.StageCounts = slices.Clone(s.StageCounts)
	s.DomainBacklogs = slices.Clone(s.DomainBacklogs)
	s.Blockages = slices.Clone(s.Blockages)
	if s.LatestFailure != nil {
		failure := *s.LatestFailure
		s.LatestFailure = &failure
	}
	return s
}

// readTerraformState returns the Terraform-state admin evidence and where it
// came from: the stored terraform_state row when every fence passes, otherwise
// the two live statements. The row has its own as_of and digest, so this
// decision is independent of the active-work one.
func (s StatusStore) readTerraformState(ctx context.Context, asOf time.Time) (statestore.TerraformStateAdminEvidence, statuspkg.ActiveWorkSource, error) {
	if s.startupErr != nil {
		return statestore.TerraformStateAdminEvidence{}, statuspkg.ActiveWorkSource{}, s.startupErr
	}
	result, err := s.summaryReader.terraformModel().Read(ctx, asOf, summary.Hooks[statestore.TerraformStateAdminEvidence]{
		Select: func(ctx context.Context) (summary.Selection, error) {
			q, done := s.read(ctx, statusReadTerraformStateModel)
			selection, err := summary.Select(ctx, q, summary.SelectConfig{
				ModelKey: summary.ModelTerraformState, SourceSHA256: statestore.SummarySourceSHA256(),
				StaleAfter: s.summaryReader.terraform.Config.StaleAfter,
			})
			return selection, done(err)
		},
		Decode: statestore.DecodeSummaryEntries,
		Live: func(ctx context.Context) (statestore.TerraformStateAdminEvidence, error) {
			q, done := s.read(ctx, statusReadTerraformState)
			evidence, err := statestore.ReadTerraformStateAdminEvidence(ctx, q, statuspkg.MaxTerraformStateRecentWarnings, asOf)
			return evidence, done(err)
		},
		Clone: func(e statestore.TerraformStateAdminEvidence) statestore.TerraformStateAdminEvidence {
			e.LastSerials = slices.Clone(e.LastSerials)
			e.RecentWarnings = slices.Clone(e.RecentWarnings)
			return e
		},
		Observe: func(ctx context.Context, selection summary.Selection) {
			summary.Observe(ctx, s.Instruments, summary.Observation{
				ModelKey: summary.ModelTerraformState, SpanPrefix: "status.terraform_state",
				Source: selection.Source, Reason: selection.Reason,
				AsOf: selection.AsOf, Age: selection.Age, SignedAge: selection.SignedAge,
			})
		},
	})
	return result.Value, statuspkg.ActiveWorkSource{
		Source: string(result.Source), Reason: string(result.Reason), AsOf: result.AsOf, Age: result.Age,
	}, err
}
