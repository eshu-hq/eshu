// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// LinkPhases splits one LinkNext call's wall time (LinkResult.Duration) by
// the step it was spent in (#9275). A link that is slow on a hosted runner
// names its step here instead of a single total: a long Commit points at WAL
// flush latency, a long Statement at the SQL or its buffers, a long Begin or
// Locks at the connection or a lock wait.
//
// The phases are measured back to back with a monotonic clock, so they never
// sum past Duration. What is left over is the cost of the Go code between
// them (context setup, row scanning outside the named calls) and is small;
// LinkResult.Duration minus Sum is that remainder. A phase a path never ran
// is zero: a break or an idle exit has no Statement.
type LinkPhases struct {
	// EnsureCursor is the autocommit insert that creates the scope's cursor
	// row. It is its own commit, outside the link transaction.
	EnsureCursor time.Duration
	// Begin is opening the link transaction.
	Begin time.Duration
	// Locks is everything the transaction does before the link statement or
	// an early cursor advance: the cursor lock, the head activation read, the
	// generation reads and locks, the prior fence, the full-link slot, and the
	// session settings.
	Locks time.Duration
	// Statement is the root, rebase or incremental link statement, including
	// the row-count check on its result. It is zero when no statement ran.
	Statement time.Duration
	// Advance is the cursor update that moves the scope past the activation.
	Advance time.Duration
	// Commit is the link transaction's commit, which waits for a durable WAL
	// flush under the default synchronous_commit.
	Commit time.Duration
}

// Sum returns the time the named phases account for. It never exceeds the
// Duration of the LinkResult that carries them.
func (p LinkPhases) Sum() time.Duration {
	return p.EnsureCursor + p.Begin + p.Locks + p.Statement + p.Advance + p.Commit
}

// phaseTimer lays consecutive phases end to end. Each mark charges the time
// since the previous mark (or since the timer started) to one phase and moves
// the mark forward, so phases cannot overlap or double count.
type phaseTimer struct {
	phases LinkPhases
	last   time.Time
}

func startPhaseTimer() *phaseTimer { return &phaseTimer{last: time.Now()} }

// mark adds the time since the previous mark to dst, one of t.phases' fields.
func (t *phaseTimer) mark(dst *time.Duration) {
	now := time.Now()
	*dst += now.Sub(t.last)
	t.last = now
}

// advanceKeeping moves the cursor past an activation that produced no link:
// the state generation stays where it was. The time before it is the Locks
// phase, the update itself the Advance phase.
func (w *LinkWriter) advanceKeeping(
	ctx context.Context, tx db.Transaction, scopeID, stateGenerationID string, seq int64, timer *phaseTimer,
) error {
	timer.mark(&timer.phases.Locks)
	err := w.advance(ctx, tx, scopeID, stateGenerationID, seq, false)
	timer.mark(&timer.phases.Advance)
	return err
}
