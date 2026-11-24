// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gomodules

import (
	"context"
	"fmt"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// GoEvidenceLockObserver is told how long one fence waited for the exclusive go-evidence
// lock, and whether the wait ended in the lock timeout (SQLSTATE 55P03). The composition
// root binds it to the fence's advisory wait histogram and timeout counter; it is a
// function so this leaf package does not import telemetry.
type GoEvidenceLockObserver func(ctx context.Context, wait time.Duration, timedOut bool)

// WithGoEvidenceLockObserver returns the store with the go-evidence lock observer. It
// leaves the receiver unchanged.
func (s Store) WithGoEvidenceLockObserver(observe GoEvidenceLockObserver) Store {
	s.observeGoEvidenceLock = observe
	return s
}

// lockGoEvidence takes the exclusive go-evidence advisory lock of one go scope on the
// fence's transaction (#11327), under the transaction's lock timeout. The key and the
// statement come from the supply-chain impact core, the same derivation the pass's
// shared lock uses. The wait is observed whether it was granted or timed out; an error
// leaves nothing detached, because the fence has not run a statement that changes data.
func (s Store) lockGoEvidence(ctx context.Context, tx db.Transaction, goScopeID string) error {
	started := time.Now()
	_, err := tx.ExecContext(ctx, supplychaincore.SupplyChainImpactGoEvidenceExclusiveLockQuery,
		supplychaincore.SupplyChainImpactGoEvidenceLockKey(goScopeID))
	if s.observeGoEvidenceLock != nil {
		s.observeGoEvidenceLock(ctx, time.Since(started), err != nil && coordination.IsLockNotAvailable(err))
	}
	if err != nil {
		return fmt.Errorf("lock go evidence: %w", err)
	}
	return nil
}
