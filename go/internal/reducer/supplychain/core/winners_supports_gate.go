// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The winners maintainer's inputs read the per-support table (#10880 PR-C1 d), so
// the maintainer waits for that table the way the writer and the readers do (R2
// of the C1 arbiter ruling). A tick that finds the table not ready claims no lease,
// writes nothing, leaves the winners watermark alone and counts itself. The first
// ready tick after such a period runs one full, because the capture functions and
// the keyed marks of the not-ready period staged nothing for a finding that had no
// support yet; that decision is durable (see SupplyChainImpactWinnersSupportsGate),
// so it survives a restart and needs no process memory.

// SupplyChainImpactWinnersSupportsGate is what one gate read reports.
type SupplyChainImpactWinnersSupportsGate struct {
	// Ready is the supports readiness predicate the writer and the readers use.
	Ready bool
	// FullNeeded is true when the supports marker completed after the winners
	// watermark was last committed: the winners were last written before the
	// table they now read was complete, so one full must run. Both stamps are
	// durable rows, read by one statement. It is meaningful only when Ready.
	FullNeeded bool
}

// SupplyChainImpactWinnersSupportsGateReader reads the gate. Implemented by
// postgres.SupplyChainImpactWinnersStore.
type SupplyChainImpactWinnersSupportsGateReader interface {
	WinnersSupportsGate(ctx context.Context) (SupplyChainImpactWinnersSupportsGate, error)
}

// path names the metric path label of a tick.
func (t winnersTick) path() string {
	if t == winnersTickKeyed {
		return telemetry.SupplyChainImpactWinnersPathKeyed
	}
	return telemetry.SupplyChainImpactWinnersPathFull
}

// supportsBlocked reads the gate and reports whether the tick must not run. A
// refused tick is counted on path. A failed gate read is returned as an error and
// runs nothing: an unreadable gate is not a ready table. A nil gate never blocks.
func (m SupplyChainImpactWinnersMaintainer) supportsBlocked(ctx context.Context, path string) (bool, error) {
	if m.SupportsGate == nil {
		return false, nil
	}
	gate, err := m.SupportsGate.WinnersSupportsGate(ctx)
	if err != nil {
		return false, fmt.Errorf("read winners supports gate: %w", err)
	}
	if !gate.Ready {
		m.Instruments.RecordSupplyChainImpactWinnersSupportsNotReady(ctx, path)
	}
	return !gate.Ready, nil
}

// supportsReadyForAudit reports whether the audit may run: the audit's repair
// path rewrites winners from the same inputs. A failed read allows nothing.
func (m SupplyChainImpactWinnersMaintainer) supportsReadyForAudit(ctx context.Context) bool {
	blocked, err := m.supportsBlocked(ctx, telemetry.SupplyChainImpactWinnersPathAudit)
	if err != nil {
		if m.Logger != nil && ctx.Err() == nil {
			m.Logger.WarnContext(ctx, "supply chain impact winners supports gate read failed before the audit", "error", err)
		}
		return false
	}
	return !blocked
}

// runContractTick is one maintainer tick: the supports gate first, then the
// contract decision (runDecidedContractTick). A ready table whose marker completed
// after the last committed resweep runs one supports-ready full here when the
// contract would otherwise run keyed. The contract-guarded full would skip, the
// contract being current, so this full re-reads the contract under the resweep
// lock and stands down only when a newer binary's contract committed after the
// decision read, as the keyed tick does. A contract-driven stand-down or full
// takes its own path.
func (m SupplyChainImpactWinnersMaintainer) runContractTick(ctx context.Context) (
	result SupplyChainImpactWinnersResweep, contract SupplyChainImpactWinnersContract, standDown bool, err error,
) {
	if m.SupportsGate == nil {
		return m.runDecidedContractTick(ctx)
	}
	gate, err := m.SupportsGate.WinnersSupportsGate(ctx)
	if err != nil {
		return SupplyChainImpactWinnersResweep{}, contract, false, fmt.Errorf("read winners supports gate: %w", err)
	}
	if !gate.Ready {
		m.sampleWatermark(ctx)
		m.sampleDirtyLog(ctx)
		m.Instruments.RecordSupplyChainImpactWinnersSupportsNotReady(ctx, telemetry.SupplyChainImpactWinnersPathKeyed)
		return SupplyChainImpactWinnersResweep{SupportsNotReady: true}, contract, false, nil
	}
	if gate.FullNeeded {
		if contract, err = m.Contract.WinnersContract(ctx); err != nil {
			return SupplyChainImpactWinnersResweep{}, contract, false, fmt.Errorf("read winners contract: %w", err)
		}
		if action, _ := decideWinnersContract(contract); action == winnersContractRunKeyed {
			var committed bool
			result, committed, err = m.runTick(ctx, winnersTickSupportsReadyFull)
			if err == nil && result.StoodDown {
				m.Instruments.RecordSupplyChainImpactWinnersStandDown(ctx)
			}
			if err == nil && committed {
				m.Instruments.RecordSupplyChainImpactWinnersFullResweep(ctx, telemetry.SupplyChainImpactWinnersFullReasonSupportsReady)
				if m.Logger != nil {
					m.Logger.InfoContext(ctx, "supply chain impact winners full resweep ran: the supports table became ready after the last committed resweep")
				}
			}
			return result, contract, false, err
		}
	}
	return m.runDecidedContractTick(ctx)
}

// noteSupportsWaiting logs the edges of a not-ready period once, on entry and on
// exit; the counter records every refused tick.
func (m SupplyChainImpactWinnersMaintainer) noteSupportsWaiting(ctx context.Context, waiting *bool, notReady bool) {
	if notReady == *waiting {
		return
	}
	*waiting = notReady
	if m.Logger == nil {
		return
	}
	if notReady {
		m.Logger.WarnContext(ctx, "supply chain impact winners maintainer waiting: the supports table is not ready, so no resweep runs",
			slog.String("domain", SupplyChainImpactWinnersDomain))
		return
	}
	m.Logger.InfoContext(ctx, "supply chain impact winners maintainer resumed: the supports table is ready",
		slog.String("domain", SupplyChainImpactWinnersDomain))
}
