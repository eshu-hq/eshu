// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The keyed-resweep half of the winners maintainer (#9805): the shared leased
// tick, the keyed tick, the audit, and their signals.

// runKeyedTick claims the resweep lease and runs one keyed resweep. It is
// RunOnce with the keyed rebuilder: the same lease, the same skip and error
// handling, the same span and signals.
func (m SupplyChainImpactWinnersMaintainer) runKeyedTick(ctx context.Context) (bool, error) {
	if m.KeyedRebuilder == nil {
		return false, fmt.Errorf("supply chain impact winners keyed tick requires a keyed rebuilder")
	}
	return m.runTick(ctx, true)
}

// runTick is the leased resweep both RunOnce (full) and runKeyedTick share.
func (m SupplyChainImpactWinnersMaintainer) runTick(ctx context.Context, keyed bool) (rebuilt bool, err error) {
	if err := m.validate(); err != nil {
		return false, err
	}
	m.sampleWatermark(ctx)
	m.sampleDirtyLog(ctx)
	claimed, err := m.LeaseManager.ClaimPartitionLease(
		ctx, SupplyChainImpactWinnersDomain, 0, 1, m.leaseOwner(), m.leaseTTL(),
	)
	if err != nil {
		return false, fmt.Errorf("claim winners lease: %w", err)
	}
	if !claimed {
		return false, nil
	}
	defer func() {
		if relErr := m.LeaseManager.ReleasePartitionLease(
			ctx, SupplyChainImpactWinnersDomain, 0, 1, m.leaseOwner(),
		); relErr != nil && err == nil {
			err = fmt.Errorf("release winners lease: %w", relErr)
		}
	}()

	stamp := m.now()
	ctx, span := m.startResweepSpan(ctx)
	started := time.Now()
	var result SupplyChainImpactWinnersResweep
	var rbErr error
	if keyed {
		result, rbErr = m.KeyedRebuilder.ResweepWinnersKeyed(ctx, stamp)
	} else {
		result, rbErr = m.Rebuilder.ResweepWinners(ctx, stamp)
	}
	elapsed := time.Since(started)
	if rbErr != nil {
		span.RecordError(rbErr)
		span.SetStatus(codes.Error, "winners resweep failed")
		span.End()
		return false, fmt.Errorf("resweep winners: %w", rbErr)
	}
	if result.Skipped {
		m.emitResweepSkipped(ctx, span)
		span.End()
		return false, nil
	}
	m.emitResweep(ctx, span, stamp, result, elapsed)
	span.End()
	return true, nil
}

// runAuditLoop runs the audit every AuditInterval until ctx is cancelled. It
// waits for the startup full resweep (fullDone) because an audit of a table
// that was never swept would call every key stale.
func (m SupplyChainImpactWinnersMaintainer) runAuditLoop(ctx context.Context, fullDone *atomic.Bool) {
	for {
		if err := m.wait(ctx, m.AuditInterval); err != nil {
			return
		}
		if !fullDone.Load() {
			continue
		}
		if err := m.runAuditOnce(ctx); err != nil && m.Logger != nil && ctx.Err() == nil {
			m.Logger.Error("supply chain impact winners audit failed", "error", err)
		}
	}
}

// runAuditOnce claims the audit lease and runs one audit. A pass that finds
// stale keys has already queued them in the dirty log, so the next keyed tick
// repairs them; this method only reports them.
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

	ctx, span := m.startResweepSpan(ctx)
	defer span.End()
	span.SetAttributes(attribute.String(telemetry.MetricDimensionPath, telemetry.SupplyChainImpactWinnersPathAudit))
	started := time.Now()
	audit, auditErr := m.Auditor.AuditWinners(ctx)
	elapsed := time.Since(started)
	if auditErr != nil {
		span.RecordError(auditErr)
		span.SetStatus(codes.Error, "winners audit failed")
		return fmt.Errorf("audit winners: %w", auditErr)
	}
	if audit.Skipped {
		span.SetAttributes(attribute.Bool("skipped", true))
		return nil
	}
	span.SetAttributes(attribute.Int64("repairs", audit.Repairs))
	if in := m.Instruments; in != nil {
		if in.SupplyChainImpactWinnersResweepDuration != nil {
			in.SupplyChainImpactWinnersResweepDuration.Record(ctx, elapsed.Seconds(),
				metric.WithAttributes(telemetry.AttrPath(telemetry.SupplyChainImpactWinnersPathAudit)))
		}
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
	return nil
}

// sampleDirtyLog feeds the dirty log's depth and oldest-row age to their
// gauges. Like sampleWatermark it runs before the lease claim on every tick, so
// an instance that holds no lease still reports the backlog. A failed read
// keeps the last sample.
func (m SupplyChainImpactWinnersMaintainer) sampleDirtyLog(ctx context.Context) {
	if m.DirtyLogReader == nil || m.Instruments == nil {
		return
	}
	stats, err := m.DirtyLogReader.DirtyLogStats(ctx)
	if err != nil {
		if m.Logger != nil {
			m.Logger.WarnContext(ctx, "supply chain impact winners dirty log stats read failed", "error", err)
		}
		return
	}
	m.Instruments.RecordSupplyChainImpactWinnersDirtyLog(stats.Depth, stats.OldestAge)
}

// recordKeyedSignals records the keyed tick's counters: keys per sweep and
// identifiers consumed, which the full path has none of, and the capture
// counter for the sources a database trigger owns, which the maintainer can only
// count when it consumes them. fact_upsert and fact_retract are counted by their
// writers at capture time. A tick that fell back to the full rebuild past the
// cap still consumed its rows, so the trigger sources count on either path.
func (m SupplyChainImpactWinnersMaintainer) recordKeyedSignals(ctx context.Context, r SupplyChainImpactWinnersResweep) {
	in := m.Instruments
	if in == nil {
		return
	}
	in.RecordSupplyChainImpactWinnersCapture(ctx, telemetry.SupplyChainImpactWinnersSourceActivation, r.TriggerCaptured.Activation)
	in.RecordSupplyChainImpactWinnersCapture(ctx, telemetry.SupplyChainImpactWinnersSourceOperator, r.TriggerCaptured.Operator)
	in.RecordSupplyChainImpactWinnersCapture(ctx, telemetry.SupplyChainImpactWinnersSourceScanner, r.TriggerCaptured.Scanner)
	if r.Path != telemetry.SupplyChainImpactWinnersPathKeyed {
		return
	}
	if in.SupplyChainImpactWinnersKeysPerSweep != nil {
		in.SupplyChainImpactWinnersKeysPerSweep.Record(ctx, r.Keys)
	}
	if in.SupplyChainImpactWinnersIdentifiersConsumed != nil && r.Identifiers > 0 {
		in.SupplyChainImpactWinnersIdentifiersConsumed.Add(ctx, r.Identifiers)
	}
}
