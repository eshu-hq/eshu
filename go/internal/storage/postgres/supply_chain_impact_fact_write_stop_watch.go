// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The supports-only impact writer must never run before the finding-fact reject trigger exists
// (#10880 PR-C2 k, arbiter-prc-design section 2): a mixed fleet of old and new writers produces
// wrong data (F11), and the trigger is what makes an old writer fail loudly instead. The reducer
// therefore reads pg_trigger.tgenabled, exports it as
// eshu_dp_supply_chain_impact_fact_write_stop_armed, and the impact handler's entry gate defers,
// non-counting, while the trigger is not enabled. The read is cached for a short interval so a pass
// costs no extra statement; a poller refreshes it so the gauge is current on an idle reducer.

// supplyChainImpactFactWriteStopArmedSQL reads whether the trigger exists on fact_records and is
// enabled for ordinary sessions ('O', or 'A' for always); a missing trigger reads false.
// $1 is the trigger name.
const supplyChainImpactFactWriteStopArmedSQL = `SELECT COALESCE((
    SELECT tgenabled IN ('O', 'A')
    FROM pg_trigger
    WHERE tgrelid = to_regclass('fact_records') AND tgname = $1 AND NOT tgisinternal
), FALSE)`

// supplyChainImpactFactWriteStopTTL is how long a read of the trigger state is trusted by Armed.
const supplyChainImpactFactWriteStopTTL = 30 * time.Second

// SupplyChainImpactFactWriteStopWatch reads and caches whether the finding-fact reject trigger is
// enabled. Safe for concurrent use. Share one value between the impact entry gate and the poller.
type SupplyChainImpactFactWriteStopWatch struct {
	// DB runs the read.
	DB db.Queryer
	// Instruments exports the armed gauge on every fresh read; nil records nothing.
	Instruments *telemetry.Instruments
	// TTL overrides the cache interval; zero uses 30 seconds.
	TTL time.Duration
	// Now supplies the clock; nil uses time.Now.
	Now func() time.Time

	mu     sync.Mutex
	armed  bool
	readAt time.Time
}

// Armed reports whether the trigger is enabled, from a read at most one TTL old. A failed read is
// returned as an error and is not cached: an unreadable trigger is not an enabled one.
func (w *SupplyChainImpactFactWriteStopWatch) Armed(ctx context.Context) (bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.readAt.IsZero() && w.now().Sub(w.readAt) < w.ttl() {
		return w.armed, nil
	}
	return w.readLocked(ctx)
}

// Refresh reads the trigger state now, whatever the cache holds, and exports the gauge. The reducer
// calls it at start and on its poll.
func (w *SupplyChainImpactFactWriteStopWatch) Refresh(ctx context.Context) (bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.readLocked(ctx)
}

func (w *SupplyChainImpactFactWriteStopWatch) readLocked(ctx context.Context) (bool, error) {
	if w.DB == nil {
		return false, fmt.Errorf("fact write stop watch database is required")
	}
	rows, err := w.DB.QueryContext(ctx, supplyChainImpactFactWriteStopArmedSQL, supplyChainImpactFactWriteRejectConstraint)
	if err != nil {
		return false, fmt.Errorf("read fact write stop trigger state: %w", err)
	}
	defer func() { _ = rows.Close() }()
	armed := false
	found := false
	for rows.Next() {
		if err := rows.Scan(&armed); err != nil {
			return false, fmt.Errorf("scan fact write stop trigger state: %w", err)
		}
		found = true
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("read fact write stop trigger state: %w", err)
	}
	if !found {
		return false, fmt.Errorf("read fact write stop trigger state: no row")
	}
	w.armed, w.readAt = armed, w.now()
	w.Instruments.RecordSupplyChainImpactFactWriteStopArmed(ctx, armed)
	return armed, nil
}

func (w *SupplyChainImpactFactWriteStopWatch) ttl() time.Duration {
	if w.TTL > 0 {
		return w.TTL
	}
	return supplyChainImpactFactWriteStopTTL
}

func (w *SupplyChainImpactFactWriteStopWatch) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}
