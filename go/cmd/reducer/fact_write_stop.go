// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The finding-fact write stop's armed check (#10880 PR-C2 k, arbiter-prc-design section 2). The
// impact handler's entry gate and the reducer's poller share one watch of the reject trigger on
// fact_records: the gate defers, non-counting, while it is not enabled, and the poller reads it at
// start and every 30 seconds so eshu_dp_supply_chain_impact_fact_write_stop_armed is current on an
// idle reducer.

// factWriteStopPollInterval is how often the poller re-reads the trigger state.
const factWriteStopPollInterval = 30 * time.Second

var (
	factWriteStopOnce  sync.Once
	factWriteStopWatch *postgres.SupplyChainImpactFactWriteStopWatch
)

// factWriteStopWatchFor returns the process's one watch of the reject trigger, built on first use
// over the reducer's database. A reducer process has one database, so the gate wired into the
// handlers and the poller started by run share it.
func factWriteStopWatchFor(database db.ExecQueryer, instruments *telemetry.Instruments) *postgres.SupplyChainImpactFactWriteStopWatch {
	factWriteStopOnce.Do(func() {
		factWriteStopWatch = &postgres.SupplyChainImpactFactWriteStopWatch{DB: database, Instruments: instruments}
	})
	return factWriteStopWatch
}

// startFactWriteStopPoll reads the trigger state now and every poll interval until ctx ends, and
// returns a wait function for shutdown before the database closes. A failed read is logged and
// retried at the next interval; the gate treats an unreadable trigger as not armed on its own.
func startFactWriteStopPoll(ctx context.Context, watch *postgres.SupplyChainImpactFactWriteStopWatch, logger *slog.Logger) func() {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(factWriteStopPollInterval)
		defer ticker.Stop()
		last := -1
		for {
			armed, err := watch.Refresh(ctx)
			switch {
			case err != nil:
				if ctx.Err() == nil && logger != nil {
					logger.Warn("supply chain impact fact write stop state unreadable", "error", err)
				}
			case logger != nil && stateChanged(&last, armed):
				if armed {
					logger.Info("supply chain impact fact write stop armed: the reject trigger on fact_records is enabled")
				} else {
					logger.Warn("supply chain impact fact write stop NOT armed: the reject trigger on fact_records is missing or disabled; impact passes defer until it is enabled")
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return func() { <-done }
}

// stateChanged records armed in last and reports whether it differs from the previous reading, so the
// poller logs the edges of the state, not every interval.
func stateChanged(last *int, armed bool) bool {
	next := 0
	if armed {
		next = 1
	}
	changed := *last != next
	*last = next
	return changed
}
