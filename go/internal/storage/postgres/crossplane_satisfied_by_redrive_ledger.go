// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// recordCrossplaneRedriveTargetQuery durably records that targetScopeID has
// had a re-drive chance against the (xrd_group, xrd_claim_kind) identity, so
// listCrossplaneRedriveTargetScopesQuery's NOT EXISTS fence skips it on every
// later sweep for the SAME identity -- including a resync of the XRD
// platform repo that changes nothing about the XRD's own (group, claim_kind).
// See migration 076's doc comment on crossplane_satisfied_by_redrive_target_ledger
// for why this is keyed by identity, not by XRD scope/generation or a
// timestamp.
const recordCrossplaneRedriveTargetQuery = `
INSERT INTO crossplane_satisfied_by_redrive_target_ledger (target_scope_id, xrd_group, xrd_claim_kind, redriven_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (target_scope_id, xrd_group, xrd_claim_kind) DO NOTHING
`

// CrossplaneRedriveTargetLedgerStore persists the durable "already re-driven"
// ledger the target-discovery query's already-satisfied fence reads.
type CrossplaneRedriveTargetLedgerStore struct {
	db  ExecQueryer
	Now func() time.Time
}

// NewCrossplaneRedriveTargetLedgerStore constructs the target ledger store.
func NewCrossplaneRedriveTargetLedgerStore(db ExecQueryer) CrossplaneRedriveTargetLedgerStore {
	return CrossplaneRedriveTargetLedgerStore{db: db}
}

func (s CrossplaneRedriveTargetLedgerStore) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// RecordRedriven marks targetScopeID as already re-driven against the
// (group, claimKind) identity. Idempotent: a pair already recorded is left
// untouched (ON CONFLICT DO NOTHING), so concurrent sweeps for the same
// identity converge without error.
func (s CrossplaneRedriveTargetLedgerStore) RecordRedriven(
	ctx context.Context,
	targetScopeID string,
	group string,
	claimKind string,
) error {
	if s.db == nil {
		return errors.New("crossplane redrive target ledger database is required")
	}
	if targetScopeID == "" || group == "" || claimKind == "" {
		return errors.New("crossplane redrive target ledger requires scope id, group, and claim kind")
	}
	if _, err := s.db.ExecContext(ctx, recordCrossplaneRedriveTargetQuery, targetScopeID, group, claimKind, s.now()); err != nil {
		return fmt.Errorf("record crossplane redrive target: %w", err)
	}
	return nil
}
