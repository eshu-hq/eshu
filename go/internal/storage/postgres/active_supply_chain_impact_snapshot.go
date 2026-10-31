// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// WithSupplyChainImpactSnapshot runs all existing impact expansion readers on
// one physical read-only repeatable-read snapshot. The callback receives a
// FactStore clone so optional FactStore loader interfaces remain available.
// The clone is valid only during the callback and does not use the identity
// cache, whose entries may have been read before this snapshot began.
func (s FactStore) WithSupplyChainImpactSnapshot(
	ctx context.Context,
	load func(factload.FactLoader) error,
) error {
	if s.database == nil {
		return fmt.Errorf("fact store database is required")
	}
	if load == nil {
		return fmt.Errorf("supply chain impact snapshot callback is required")
	}
	return withReadOnlyRepeatableRead(ctx, s.database, func(queryer db.Queryer) error {
		bound := &impactSnapshotQueryer{queryer: queryer}
		bound.active.Store(true)
		defer bound.active.Store(false)
		clone := s
		clone.database = bound
		clone.identityCache = nil
		clone.impactStaticSnapshot = true
		return load(&clone)
	})
}

type impactSnapshotQueryer struct {
	queryer db.Queryer
	active  atomic.Bool
}

func (q *impactSnapshotQueryer) QueryContext(
	ctx context.Context, statement string, args ...any,
) (db.Rows, error) {
	if !q.active.Load() {
		return nil, fmt.Errorf("supply chain impact snapshot has ended")
	}
	return q.queryer.QueryContext(ctx, statement, args...)
}

func (*impactSnapshotQueryer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, fmt.Errorf("supply chain impact snapshot is read-only")
}

func (q *impactSnapshotQueryer) BeginReadOnlyRepeatableRead(context.Context) (db.Transaction, error) {
	if !q.active.Load() {
		return nil, fmt.Errorf("supply chain impact snapshot has ended")
	}
	return impactNestedReadTransaction{impactSnapshotQueryer: q}, nil
}

type impactNestedReadTransaction struct{ *impactSnapshotQueryer }

func (impactNestedReadTransaction) Commit() error   { return nil }
func (impactNestedReadTransaction) Rollback() error { return nil }

// protectedImpactStaticFactSQL places the derived-scope exclusion in each
// fact_records branch before its LIMIT. A changed query shape fails closed.
func (s FactStore) protectedImpactStaticFactSQL(statement string) (string, error) {
	if !s.impactStaticSnapshot {
		return statement, nil
	}
	const source = "WHERE fact."
	branches := strings.Count(statement, "FROM fact_records AS fact")
	if branches == 0 || strings.Count(statement, source) != branches {
		return "", fmt.Errorf("protected impact static fact query shape is unsupported")
	}
	return strings.ReplaceAll(statement, source,
		"WHERE fact.scope_id NOT LIKE 'image://derived/%'\n  AND fact."), nil
}
