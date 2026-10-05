// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// State is the lifecycle state of one activation obligation row.
type State string

// Obligation states. pending and leased are open; completed and obsolete are
// terminal and wait only for Prune.
const (
	StatePending   State = "pending"
	StateLeased    State = "leased"
	StateCompleted State = "completed"
	StateObsolete  State = "obsolete"
	// StateInapplicable is terminal for a generation that can never carry a
	// backward-evidence phase. Only the generation cascade removes it.
	StateInapplicable State = "inapplicable"
)

// AllStates lists every obligation state in a fixed order, for gauges that
// must report zero for a state with no rows.
var AllStates = []State{StatePending, StateLeased, StateCompleted, StateObsolete, StateInapplicable}

// Outcome is why one Finalize call ended. It is a closed set, safe as a
// metric label.
type Outcome string

// Finalize outcomes.
const (
	// OutcomeCompleted: the exact backward-evidence phase exists, every
	// waiting deployment_mapping row was woken, no handler for the generation
	// is in flight, and the token-fenced completion committed.
	OutcomeCompleted Outcome = "completed"
	// OutcomePhaseNotReady: the scope still points at the generation but its
	// own backward-evidence phase does not exist. Nothing was written; the
	// lease stays held until it expires.
	OutcomePhaseNotReady Outcome = "phase_not_ready"
	// OutcomeWorkPending: the phase exists and up to WakeBatchLimit rows were
	// woken and committed, but a handler for the generation is still claimed
	// or running, or more waiting rows remain. The obligation stays open.
	OutcomeWorkPending Outcome = "work_pending"
	// OutcomeObsolete: the scope no longer points at the generation, so the
	// obligation retired without waking anything.
	OutcomeObsolete Outcome = "obsolete"
	// OutcomeNotOwner: the caller's owner, token or lease no longer matches
	// the row (expired, reclaimed, already finished, or forged). Nothing was
	// written.
	OutcomeNotOwner Outcome = "not_owner"
	// OutcomeMissing: no scope row or no obligation row exists for the
	// identity. Nothing was written.
	OutcomeMissing Outcome = "missing"
	// OutcomeInapplicable: the generation has no repository fact (or the
	// maintainer found no repository maps to it), so no pass can publish its
	// phase; the obligation retired as inapplicable under the lease fence.
	OutcomeInapplicable Outcome = "inapplicable"
)

// WakeBatchLimit caps how many waiting deployment_mapping rows one Finalize
// wakes. Rows beyond the cap stay future-deferred and keep the obligation
// open, so the next Finalize wakes the next batch.
const WakeBatchLimit = 32

// ErrLeaseLost reports that the obligation lease expired while Finalize held
// its transaction, after it had already woken rows. The transaction rolled
// back, so the wake did not survive, and the next owner repeats it.
var ErrLeaseLost = errors.New("activation obligation lease expired during finalize")

// Obligation is one claimed activation obligation: the exact scope generation
// to maintain, and the lease identity that fences its completion.
type Obligation struct {
	ScopeID      string
	GenerationID string
	LeaseOwner   string
	LeaseToken   int64
	LeaseUntil   time.Time
	// CreatedAt is when the obligation was owed (Ack or catch-up time).
	CreatedAt time.Time
}

// FinalizeResult reports one Finalize call.
type FinalizeResult struct {
	Outcome Outcome
	// Woken counts the deployment_mapping rows whose wake committed.
	Woken int
}

// Database is the storage surface the obligation store needs: plain reads
// and writes plus transactions.
type Database interface {
	db.ExecQueryer
	db.Beginner
}

// Store claims, finalizes, catches up and prunes activation obligations.
type Store struct {
	database Database
}

// NewStore returns a Store backed by database.
func NewStore(database Database) Store {
	return Store{database: database}
}

// Insert records the activation obligation for one scope generation. Callers
// run it inside the transaction that activates the generation, so the
// obligation commits or rolls back with the activation. A repeated insert for
// the same generation is a no-op.
func Insert(ctx context.Context, executor db.Executor, scopeID, generationID, workItemID string) error {
	if _, err := executor.ExecContext(ctx, insertObligationQuery, scopeID, generationID, workItemID); err != nil {
		return fmt.Errorf("insert exact activation obligation: %w", err)
	}
	return nil
}

// Claim leases the oldest claimable obligation to owner for lease. It returns
// nil when nothing is claimable. A pending row, or a leased row whose lease
// has expired on the database clock, is claimable; claiming bumps the row's
// claim_token, so every earlier holder is fenced out.
func (s Store) Claim(ctx context.Context, owner string, lease time.Duration) (*Obligation, error) {
	if strings.TrimSpace(owner) == "" || lease <= 0 {
		return nil, errors.New("claim activation obligation: owner and positive lease required")
	}
	rows, err := s.database.QueryContext(ctx, claimObligationQuery, owner,
		float64(lease)/float64(time.Millisecond))
	if err != nil {
		return nil, fmt.Errorf("claim activation obligation: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("claim activation obligation: %w", err)
		}
		return nil, nil
	}
	var work Obligation
	if err := rows.Scan(&work.ScopeID, &work.GenerationID, &work.LeaseOwner,
		&work.LeaseToken, &work.LeaseUntil, &work.CreatedAt); err != nil {
		return nil, fmt.Errorf("claim activation obligation: scan: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim activation obligation: %w", err)
	}
	return &work, nil
}
