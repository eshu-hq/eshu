// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// defaultSupplyChainImpactWinnersAuditTickBudget bounds one audit tick of a
// range-sweep auditor (#11131): with ranges of about 14 s at QA shape, five
// minutes of every ten-minute interval completes a sweep of about 840 ranges
// in about 7 h, inside the 24 h bar.
const defaultSupplyChainImpactWinnersAuditTickBudget = 5 * time.Minute

func (m SupplyChainImpactWinnersMaintainer) auditTickBudget() time.Duration {
	if m.AuditTickBudget > 0 {
		return m.AuditTickBudget
	}
	return defaultSupplyChainImpactWinnersAuditTickBudget
}

// runAuditLoop runs the audit every AuditInterval until ctx is cancelled,
// only while the stored winners contract matches this binary's (#11117): an
// audit of a table no full has swept under this contract would call keys
// stale that the pending full is about to rewrite, and a binary standing down
// writes nothing.
func (m SupplyChainImpactWinnersMaintainer) runAuditLoop(ctx context.Context) {
	for {
		if err := m.wait(ctx, m.AuditInterval); err != nil {
			return
		}
		if !m.auditAllowed(ctx) {
			continue
		}
		if err := m.runAuditOnce(ctx); err != nil && m.Logger != nil && ctx.Err() == nil {
			m.Logger.Error("supply chain impact winners audit failed", "error", err)
		}
	}
}

// auditAllowed reports whether the stored contract matches this binary's. A
// failed read allows nothing; the next interval reads again.
func (m SupplyChainImpactWinnersMaintainer) auditAllowed(ctx context.Context) bool {
	if m.Contract == nil {
		return false
	}
	contract, err := m.Contract.WinnersContract(ctx)
	if err != nil {
		if m.Logger != nil && ctx.Err() == nil {
			m.Logger.WarnContext(ctx, "supply chain impact winners contract read failed before the audit", "error", err)
		}
		return false
	}
	action, _ := decideWinnersContract(contract)
	return action == winnersContractRunKeyed
}

// runAuditOnce claims the audit lease and runs one audit tick. A whole-table
// auditor runs one pass. A range-sweep auditor (#11131) runs ranges back to
// back while it reports More, the tick budget lasts, and a renewal of the
// lease by the same owner succeeds: a lost lease ends the tick, and the
// cursor's compare-and-set keeps an overlapping instance from advancing a
// range twice. A pass that finds stale keys has already queued them, so the
// next keyed tick repairs them; this method only reports them.
func (m SupplyChainImpactWinnersMaintainer) runAuditOnce(ctx context.Context) (err error) {
	if m.Auditor == nil {
		return nil
	}
	if m.LeaseManager == nil {
		return fmt.Errorf("supply chain impact winners audit requires a lease manager")
	}
	claimed, err := m.LeaseManager.ClaimPartitionLease(
		ctx, SupplyChainImpactWinnersAuditDomain, 0, 1, m.leaseOwner(), m.leaseTTL(),
	)
	if err != nil {
		return fmt.Errorf("claim winners audit lease: %w", err)
	}
	if !claimed {
		return nil
	}
	defer func() {
		if relErr := m.LeaseManager.ReleasePartitionLease(
			ctx, SupplyChainImpactWinnersAuditDomain, 0, 1, m.leaseOwner(),
		); relErr != nil && err == nil {
			err = fmt.Errorf("release winners audit lease: %w", relErr)
		}
	}()
	deadline := time.Now().Add(m.auditTickBudget())
	for {
		audit, err := m.runAuditPass(ctx)
		if err != nil {
			return err
		}
		if !audit.More || ctx.Err() != nil || !time.Now().Before(deadline) {
			return nil
		}
		renewed, err := m.LeaseManager.ClaimPartitionLease(
			ctx, SupplyChainImpactWinnersAuditDomain, 0, 1, m.leaseOwner(), m.leaseTTL(),
		)
		if err != nil {
			return fmt.Errorf("renew winners audit lease: %w", err)
		}
		if !renewed {
			if m.Logger != nil {
				m.Logger.WarnContext(ctx, "supply chain impact winners audit lease lost mid-tick; the sweep resumes from the stored cursor")
			}
			return nil
		}
	}
}

// runAuditPass runs one auditor pass under the held lease and records it. A
// partial pass (one range of an unfinished sweep) records its range signals
// only: no audit commit, no last-success reset.
func (m SupplyChainImpactWinnersMaintainer) runAuditPass(ctx context.Context) (SupplyChainImpactWinnersAudit, error) {
	ctx, span := m.startResweepSpan(ctx)
	defer span.End()
	span.SetAttributes(attribute.String(telemetry.MetricDimensionPath, telemetry.SupplyChainImpactWinnersPathAudit))
	started := time.Now()
	end := m.Instruments.BeginSupplyChainImpactWinnersResweep(telemetry.SupplyChainImpactWinnersPathAudit)
	audit, auditErr := m.Auditor.AuditWinners(ctx)
	end()
	elapsed := time.Since(started)
	if auditErr != nil {
		span.RecordError(auditErr)
		span.SetStatus(codes.Error, "winners audit failed")
		return SupplyChainImpactWinnersAudit{}, fmt.Errorf("audit winners: %w", auditErr)
	}
	if audit.Outcome != "" {
		span.SetAttributes(attribute.String(telemetry.MetricDimensionOutcome, audit.Outcome), attribute.Bool("partial", audit.Partial))
		if in := m.Instruments; in != nil && in.SupplyChainImpactWinnersAuditRangeDuration != nil {
			in.SupplyChainImpactWinnersAuditRangeDuration.Record(ctx, elapsed.Seconds(),
				metric.WithAttributes(attribute.String(telemetry.MetricDimensionOutcome, audit.Outcome)))
		}
	}
	// A deferred audit never reached the writer with a result: the tick
	// ends here with no fallback to the writer scan. It is checked before
	// Skipped because a deferred pass never attempted the lock. The refusal
	// itself is counted at the reader by reason; the pass records no
	// duration, no repairs, and no skip.
	if audit.Deferred {
		span.SetAttributes(attribute.Bool("deferred", true))
		if m.Logger != nil {
			m.Logger.InfoContext(ctx, "supply chain impact winners audit deferred")
		}
		return audit, nil
	}
	if audit.Skipped {
		span.SetAttributes(attribute.Bool("skipped", true))
		if in := m.Instruments; in != nil && in.SupplyChainImpactWinnersAuditSkipped != nil {
			in.SupplyChainImpactWinnersAuditSkipped.Add(ctx, 1)
		}
		return audit, nil
	}
	span.SetAttributes(attribute.Int64("repairs", audit.Repairs))
	if in := m.Instruments; in != nil {
		if audit.Repairs > 0 && in.SupplyChainImpactWinnersBackstopRepairs != nil {
			in.SupplyChainImpactWinnersBackstopRepairs.Add(ctx, audit.Repairs)
		}
		// The audit is the writer of its own rows, so it counts them here.
		in.RecordSupplyChainImpactWinnersCapture(ctx, telemetry.SupplyChainImpactWinnersSourceAudit, audit.Repairs)
	}
	if audit.Repairs > 0 && m.Logger != nil {
		m.Logger.WarnContext(ctx, "supply chain impact winners audit found stale keys",
			slog.Int64("repairs", audit.Repairs),
			slog.Float64("read_seconds", audit.ReadDuration.Seconds()),
			slog.Float64("duration_seconds", elapsed.Seconds()),
		)
	}
	if audit.Partial {
		return audit, nil
	}
	m.Instruments.RecordSupplyChainImpactWinnersCommit(telemetry.SupplyChainImpactWinnersPathAudit, time.Now())
	if in := m.Instruments; in != nil && in.SupplyChainImpactWinnersResweepDuration != nil {
		in.SupplyChainImpactWinnersResweepDuration.Record(ctx, elapsed.Seconds(),
			metric.WithAttributes(telemetry.AttrPath(telemetry.SupplyChainImpactWinnersPathAudit)))
	}
	if m.Logger != nil {
		// One line per completed pass, zero repairs included (#11117), so an
		// audit that runs clean is visible next to one that never runs.
		m.Logger.InfoContext(ctx, "supply chain impact winners audit completed",
			slog.Int64("repairs", audit.Repairs),
			slog.Float64("read_seconds", audit.ReadDuration.Seconds()),
			slog.Float64("duration_seconds", elapsed.Seconds()),
		)
	}
	return audit, nil
}
