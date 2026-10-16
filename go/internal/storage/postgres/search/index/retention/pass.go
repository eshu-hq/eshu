// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package indexretention

import (
	"context"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Pass is one retention pass's pre-delete: it drains generations oldest
// first under one Budget and remembers which ones drained, so the prune that
// follows admits only those. The zero value is unusable; build it with
// NewPass. A Pass is not safe for concurrent use.
type Pass struct {
	budget  Budget
	logger  *slog.Logger
	reports map[string]Report
	ran     bool
}

// NewPass returns a pass bounded by rowCap deleted rows and the deadline
// (zero for none). logger, when non-nil, receives one completion line per
// generation that deleted rows or did not drain.
func NewPass(rowCap int64, deadline time.Time, logger *slog.Logger) *Pass {
	return &Pass{
		budget:  Budget{RowCap: rowCap, Deadline: deadline},
		logger:  logger,
		reports: make(map[string]Report),
	}
}

// Run drains generations in the given order, which must be oldest first, so
// a spent budget leaves the newest undrained. A generation the budget never
// reaches stays undrained. It returns the first error a batch returns, after
// recording what that generation deleted.
func (p *Pass) Run(ctx context.Context, database db.Beginner, generations []Generation) error {
	for _, generation := range generations {
		if _, seen := p.reports[generation.GenerationID]; seen {
			continue
		}
		if p.budget.Exhausted(time.Now()) {
			return nil
		}
		p.ran = true
		started := time.Now()
		report, err := Drain(ctx, database, generation, &p.budget)
		p.reports[generation.GenerationID] = report
		p.log(ctx, report, time.Since(started))
		if err != nil {
			return err
		}
	}
	return nil
}

// Ran reports whether the pass attempted at least one generation.
func (p *Pass) Ran() bool { return p != nil && p.ran }

// Drained reports whether the pass drained the generation, so the prune may
// admit it. A generation the pass never attempted is not drained.
func (p *Pass) Drained(generationID string) bool {
	if p == nil {
		return false
	}
	report, ok := p.reports[generationID]
	return ok && report.Drained()
}

// Report returns the pass's report for one generation, if it attempted it.
func (p *Pass) Report(generationID string) (Report, bool) {
	if p == nil {
		return Report{}, false
	}
	report, ok := p.reports[generationID]
	return report, ok
}

// Totals sums the pass: rows deleted per table, committed batch transactions,
// and the keyset and re-sweep wall time.
func (p *Pass) Totals() (terms, documents int64, batches int, keyset, sweep time.Duration) {
	if p == nil {
		return 0, 0, 0, 0, 0
	}
	for _, report := range p.reports {
		terms += report.TermsDeleted
		documents += report.DocumentsDeleted
		keyset += report.KeysetDuration
		sweep += report.SweepDuration
	}
	return terms, documents, p.budget.Batches(), keyset, sweep
}

// log writes one generation's completion line. A generation that drained
// without deleting a row logs at debug: most generations carry no search
// index rows, and a hundred empty lines per pass would bury the useful ones.
func (p *Pass) log(ctx context.Context, report Report, duration time.Duration) {
	if p.logger == nil {
		return
	}
	level := slog.LevelInfo
	if report.Drained() && report.Rows() == 0 {
		level = slog.LevelDebug
	}
	p.logger.Log(ctx, level, "generation retention search index pre-delete completed",
		slog.String(telemetry.LogKeyScopeID, report.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, report.GenerationID),
		slog.String("outcome", string(report.Outcome)),
		slog.Bool("drained", report.Drained()),
		slog.Int("batches", report.Batches),
		slog.Int64("terms_rows_deleted", report.TermsDeleted),
		slog.Int64("documents_rows_deleted", report.DocumentsDeleted),
		slog.Float64("duration_seconds", duration.Seconds()),
		slog.Float64("sweep_seconds", report.SweepDuration.Seconds()),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	)
}
