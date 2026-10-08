// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// pruneProducerQuery deletes up to $2 finished producer obligations whose
// finished_at is older than $1 milliseconds on the database clock. It locks
// only finished rows and skips any a concurrent replayed settle holds. Only
// completed and obsolete rows are pruned: inapplicable rows stay until their
// generation's cascade.
const pruneProducerQuery = `
WITH doomed AS (
    SELECT generation_id, scope_id
    FROM producer_activation_obligations
    WHERE state IN ('completed', 'obsolete')
      AND finished_at < clock_timestamp() - ($1::double precision * interval '1 millisecond')
    ORDER BY finished_at
    LIMIT $2
    FOR UPDATE SKIP LOCKED
)
DELETE FROM producer_activation_obligations AS obligation
USING doomed
WHERE obligation.generation_id = doomed.generation_id
  AND obligation.scope_id = doomed.scope_id
  AND obligation.state IN ('completed', 'obsolete')
`

// statsProducerQuery counts producer obligations per state and reads the age
// of the oldest open obligation on the database clock. GROUP BY state reads
// the whole table. Its size is the open rows, plus completed and obsolete
// rows inside the prune's retention window, plus inapplicable rows, which
// are never pruned and live until retention deletes their generation.
const statsProducerQuery = `
SELECT state, count(*),
    COALESCE(EXTRACT(EPOCH FROM clock_timestamp() - min(created_at)
        FILTER (WHERE state IN ('pending', 'leased'))), 0)::double precision
FROM producer_activation_obligations
GROUP BY state
`

// ProducerState is one producer_activation_obligations row state, mirroring
// the table's CHECK constraint. Settle-call outcomes that write nothing
// (not_owner, missing) are ProducerOutcomes, never states.
type ProducerState string

// Producer obligation row states.
const (
	ProducerStatePending      ProducerState = "pending"
	ProducerStateLeased       ProducerState = "leased"
	ProducerStateCompleted    ProducerState = "completed"
	ProducerStateObsolete     ProducerState = "obsolete"
	ProducerStateInapplicable ProducerState = "inapplicable"
)

// producerAllStates lists every producer row state, so the census always
// carries the full closed set.
var producerAllStates = []ProducerState{
	ProducerStatePending, ProducerStateLeased, ProducerStateCompleted,
	ProducerStateObsolete, ProducerStateInapplicable,
}

// ProducerStats is a point-in-time census of the producer obligation table.
type ProducerStats struct {
	// ByState counts rows per state; every producer state is present.
	ByState map[ProducerState]int64
	// OldestOpenAge is the age of the oldest pending or leased obligation;
	// zero when none is open.
	OldestOpenAge time.Duration
}

// PruneProducer deletes up to limit finished producer obligations older
// than retention.
func (s Store) PruneProducer(ctx context.Context, retention time.Duration, limit int) (int, error) {
	if limit <= 0 || retention < 0 {
		return 0, errors.New("prune producer activation obligations: positive limit and non-negative retention required")
	}
	deleted, err := execRows(ctx, s.database, pruneProducerQuery,
		float64(retention)/float64(time.Millisecond), limit)
	if err != nil {
		return 0, fmt.Errorf("prune producer activation obligations: %w", err)
	}
	return int(deleted), nil
}

// StatsProducer reads the per-state producer census and the oldest open
// obligation age.
//
// There is deliberately no producer catch-up. The #7584 catch-up is safe
// only because the backward-evidence phase persists: once a generation has
// its phase, catch-up never re-owes it, so catch-up plus prune converges.
// The producer signal has no equivalent persistent marker — a generation
// whose consumers already replayed via the epoch pass is indistinguishable
// from one that never replayed — so a catch-up would re-owe every pruned
// generation forever. Generations activated before the Ack insert shipped
// replay their consumers through the epoch whole pass exactly as before;
// every activation after it owes its obligation inside the Ack transaction,
// which commits or rolls back atomically with the activation, leaving no
// crash gap for a catch-up to close.
func (s Store) StatsProducer(ctx context.Context) (ProducerStats, error) {
	rows, err := s.database.QueryContext(ctx, statsProducerQuery)
	if err != nil {
		return ProducerStats{}, fmt.Errorf("read producer activation stats: %w", err)
	}
	defer func() { _ = rows.Close() }()
	stats := ProducerStats{ByState: make(map[ProducerState]int64, len(producerAllStates))}
	for _, state := range producerAllStates {
		stats.ByState[state] = 0
	}
	var oldest float64
	for rows.Next() {
		var state string
		var count int64
		var age float64
		if err := rows.Scan(&state, &count, &age); err != nil {
			return ProducerStats{}, fmt.Errorf("read producer activation stats: scan: %w", err)
		}
		stats.ByState[ProducerState(state)] = count
		if age > oldest {
			oldest = age
		}
	}
	if err := rows.Err(); err != nil {
		return ProducerStats{}, fmt.Errorf("read producer activation stats: %w", err)
	}
	stats.OldestOpenAge = time.Duration(oldest * float64(time.Second))
	return stats, nil
}
