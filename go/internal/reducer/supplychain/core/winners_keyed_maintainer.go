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

// The keyed-resweep half of the winners maintainer (#9805): the shared leased
// tick, the keyed tick, the audit, and their signals.

// runKeyedTick claims the resweep lease and runs one keyed resweep, reporting
// what it did. It is RunOnce with the keyed rebuilder: the same lease, the
// same skip and error handling, the same span and signals. The Run loop reads
// Bounded from the result to re-tick immediately while rows are queued.
func (m SupplyChainImpactWinnersMaintainer) runKeyedTick(ctx context.Context) (SupplyChainImpactWinnersResweep, error) {
	if m.KeyedRebuilder == nil {
		return SupplyChainImpactWinnersResweep{}, fmt.Errorf("supply chain impact winners keyed tick requires a keyed rebuilder")
	}
	result, _, err := m.runTick(ctx, winnersTickKeyed)
	return result, err
}

// winnersTick names what one leased tick runs.
type winnersTick int

const (
	// winnersTickFull is the unguarded full resweep (RunOnce, the #9774
	// maintainer without a keyed rebuilder).
	winnersTickFull winnersTick = iota
	// winnersTickGuardedFull is the contract-guarded full (#11117).
	winnersTickGuardedFull
	// winnersTickKeyed is the keyed tick (#9805).
	winnersTickKeyed
)

// runTick is the leased resweep RunOnce, the guarded full and runKeyedTick
// share.
func (m SupplyChainImpactWinnersMaintainer) runTick(ctx context.Context, tick winnersTick) (result SupplyChainImpactWinnersResweep, rebuilt bool, err error) {
	if err := m.validate(); err != nil {
		return SupplyChainImpactWinnersResweep{}, false, err
	}
	m.sampleWatermark(ctx)
	m.sampleDirtyLog(ctx)
	if blocked, gateErr := m.supportsBlocked(ctx, tick.path()); gateErr != nil || blocked {
		return SupplyChainImpactWinnersResweep{SupportsNotReady: blocked}, false, gateErr
	}
	claimed, err := m.LeaseManager.ClaimPartitionLease(
		ctx, SupplyChainImpactWinnersDomain, 0, 1, m.leaseOwner(), m.leaseTTL(),
	)
	if err != nil {
		return SupplyChainImpactWinnersResweep{}, false, fmt.Errorf("claim winners lease: %w", err)
	}
	if !claimed {
		return SupplyChainImpactWinnersResweep{}, false, nil
	}
	defer func() {
		if relErr := m.LeaseManager.ReleasePartitionLease(
			ctx, SupplyChainImpactWinnersDomain, 0, 1, m.leaseOwner(),
		); relErr != nil && err == nil {
			err = fmt.Errorf("release winners lease: %w", relErr)
		}
	}()

	ctx, span := m.startResweepSpan(ctx)
	started := time.Now()
	var rbErr error
	switch tick {
	case winnersTickKeyed:
		end := m.Instruments.BeginSupplyChainImpactWinnersResweep(telemetry.SupplyChainImpactWinnersPathKeyed)
		result, rbErr = m.KeyedRebuilder.ResweepWinnersKeyed(ctx)
		end()
	case winnersTickGuardedFull:
		end := m.Instruments.BeginSupplyChainImpactWinnersResweep(telemetry.SupplyChainImpactWinnersPathFull)
		result, rbErr = m.Contract.ResweepWinnersUnlessCurrent(ctx)
		end()
	default:
		end := m.Instruments.BeginSupplyChainImpactWinnersResweep(telemetry.SupplyChainImpactWinnersPathFull)
		result, rbErr = m.Rebuilder.ResweepWinners(ctx)
		end()
	}
	elapsed := time.Since(started)
	if rbErr != nil {
		span.RecordError(rbErr)
		span.SetStatus(codes.Error, "winners resweep failed")
		span.End()
		return SupplyChainImpactWinnersResweep{}, false, fmt.Errorf("resweep winners: %w", rbErr)
	}
	if result.StoodDown {
		span.SetAttributes(attribute.Bool("stood_down", true))
		if m.Logger != nil {
			m.Logger.WarnContext(ctx, "supply chain impact winners keyed tick stood down under the lock: a newer winners contract committed after the decision read")
		}
		span.End()
		return result, false, nil
	}
	if result.ContractCurrent {
		span.SetAttributes(attribute.Bool("contract_current", true))
		if m.Logger != nil {
			m.Logger.InfoContext(ctx, "supply chain impact winners full resweep not needed: the stored contract is current")
		}
		span.End()
		return result, false, nil
	}
	if result.Skipped {
		m.emitResweepSkipped(ctx, span)
		span.End()
		return result, false, nil
	}
	// The gauge ages the watermark the tick committed, not the Go clock that
	// scheduled it; a rebuilder that predates the committed stamp leaves the
	// maintainer on its own clock as before.
	stamp := result.Watermark
	if stamp.IsZero() {
		stamp = m.now()
	}
	m.emitResweep(ctx, span, stamp, result, elapsed)
	span.End()
	return result, true, nil
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
	m.Instruments.RecordSupplyChainImpactWinnersStaging(stats.StagingDepth, stats.StagingOldestAge, stats.StagingDepthUpperBound)
}

// recordKeyedSignals records the keyed tick's counters: keys per sweep and
// identifiers consumed, which are recorded for the keyed path only (a full
// resweep's drained marks are deliberately not counted as consumed), and the capture
// counter for the sources a database trigger owns, which the maintainer can only
// count when it consumes them. fact_upsert and fact_retract are counted by their
// writers once the writer's transaction commits. A tick that fell back to the full rebuild past the
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
	if r.Bounded && in.SupplyChainImpactWinnersChunkedTicks != nil {
		in.SupplyChainImpactWinnersChunkedTicks.Add(ctx, 1)
	}
}

// winnersContractAction is what one keyed-maintainer tick runs, decided from
// the persisted winners contract (#11117).
type winnersContractAction int

const (
	// winnersContractRunFull runs the guarded full: no watermark row, or a
	// NULL or lower stored version.
	winnersContractRunFull winnersContractAction = iota
	// winnersContractRunKeyed runs a keyed tick: the stored version equals
	// this binary's.
	winnersContractRunKeyed
	// winnersContractStandDown runs nothing: a newer binary's contract is
	// stored, so this binary's statements must not touch the table.
	winnersContractStandDown
)

// decideWinnersContract maps the stored contract to the tick's action and,
// for a full, its start reason. The decision reads only versions: the hash of
// the covered statements (the rebuild, the keyed build and apply statements and
// the scope-remainder convert) is pinned to the version by a test. A re-pin
// under the same version, allowed only with a byte-identical winners
// differential (#11145), leaves the stored hash stale until the next full;
// that is not a reason to run one. The capture owners, the drains and the
// staged audit are not pinned: a change to them that alters which keys get
// marked or visited needs a version bump by the rule on
// SupplyChainImpactWinnersContractVersion.
func decideWinnersContract(c SupplyChainImpactWinnersContract) (winnersContractAction, string) {
	switch {
	case !c.Present:
		return winnersContractRunFull, telemetry.SupplyChainImpactWinnersFullReasonNoWatermark
	case !c.HasVersion || c.StoredVersion < c.BinaryVersion:
		return winnersContractRunFull, telemetry.SupplyChainImpactWinnersFullReasonContractVersion
	case c.StoredVersion == c.BinaryVersion:
		return winnersContractRunKeyed, ""
	default:
		return winnersContractStandDown, ""
	}
}

// runDecidedContractTick reads the stored contract, records it, and runs what it
// decides. The full-resweep counter and the contract log line mark only a
// guarded full this tick took the lease for and committed (#11177). A tick
// that did not claim the lease, a full skipped behind another instance, and a
// full found already current under the lock all record nothing: that
// instance's commit satisfies them, and the next tick re-reads the row and
// runs keyed. standDown reports a tick that ran nothing because a newer
// contract is stored.
func (m SupplyChainImpactWinnersMaintainer) runDecidedContractTick(ctx context.Context) (result SupplyChainImpactWinnersResweep, contract SupplyChainImpactWinnersContract, standDown bool, err error) {
	contract, err = m.Contract.WinnersContract(ctx)
	if err != nil {
		return SupplyChainImpactWinnersResweep{}, contract, false, fmt.Errorf("read winners contract: %w", err)
	}
	stored := contract.StoredVersion
	if !contract.HasVersion {
		stored = 0
	}
	m.Instruments.RecordSupplyChainImpactWinnersContract(stored, contract.BinaryVersion)
	action, reason := decideWinnersContract(contract)
	switch action {
	case winnersContractStandDown:
		// The gauges keep reporting the table this binary no longer touches.
		m.sampleWatermark(ctx)
		m.sampleDirtyLog(ctx)
		m.Instruments.RecordSupplyChainImpactWinnersStandDown(ctx)
		return SupplyChainImpactWinnersResweep{}, contract, true, nil
	case winnersContractRunKeyed:
		result, err = m.runKeyedTick(ctx)
		if err == nil && result.StoodDown {
			// The next tick's decision read sees the newer version and
			// takes the stand-down branch above, with its edge log.
			m.Instruments.RecordSupplyChainImpactWinnersStandDown(ctx)
			return result, contract, false, nil
		}
		if err == nil && !result.Skipped && result.Path == telemetry.SupplyChainImpactWinnersPathFull {
			m.Instruments.RecordSupplyChainImpactWinnersFullResweep(ctx, telemetry.SupplyChainImpactWinnersFullReasonKeyedCap)
		}
		return result, contract, false, err
	default:
		var committed bool
		result, committed, err = m.runTick(ctx, winnersTickGuardedFull)
		if err == nil && committed {
			m.Instruments.RecordSupplyChainImpactWinnersFullResweep(ctx, reason)
			if m.Logger != nil {
				m.Logger.InfoContext(ctx, "supply chain impact winners full resweep recorded the contract",
					slog.String("reason", reason),
					slog.Int64("stored_version", contract.StoredVersion),
					slog.Bool("stored_version_present", contract.HasVersion),
					slog.Int64("binary_version", contract.BinaryVersion),
				)
			}
		}
		return result, contract, false, err
	}
}

// noteStandDown logs the edges of a stand-down, once on entry and once on
// exit, so a stand-down that lasts many ticks logs twice, not every tick. The
// counter records every stand-down tick.
func (m SupplyChainImpactWinnersMaintainer) noteStandDown(ctx context.Context, standingDown *bool, down bool, contract SupplyChainImpactWinnersContract) {
	if down == *standingDown {
		return
	}
	*standingDown = down
	if m.Logger == nil {
		return
	}
	if down {
		m.Logger.WarnContext(ctx, "supply chain impact winners maintainer standing down: a newer winners contract is stored",
			slog.Int64("stored_version", contract.StoredVersion),
			slog.Int64("binary_version", contract.BinaryVersion),
		)
		return
	}
	m.Logger.InfoContext(ctx, "supply chain impact winners maintainer resumed: the stored contract no longer outranks this binary",
		slog.Int64("stored_version", contract.StoredVersion),
		slog.Int64("binary_version", contract.BinaryVersion),
	)
}

// defaultSupplyChainImpactWinnersAuditTickBudget bounds one audit tick of a
// range-sweep auditor (#11131). The audit loop waits AuditInterval and then
// runs one tick, so at the 10 min default ranges run 5 of every 15 minutes:
// about 840 QA ranges of about 14.5 s are projected at about 10 h per sweep
// while ranges rarely need the resweep lock, and 51 h or more if most do
// (arbiter ruling 11131c keeps this default; operators tune by env).
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
	return action == winnersContractRunKeyed && m.supportsReadyForAudit(ctx)
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
	deadline := m.budgetNow().Add(m.auditTickBudget())
	for {
		audit, err := m.runAuditPass(ctx)
		if err != nil {
			return err
		}
		if !audit.More || ctx.Err() != nil || !m.budgetNow().Before(deadline) {
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
