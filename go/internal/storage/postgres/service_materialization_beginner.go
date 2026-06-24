// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// ServiceMaterializationBeginner adapts a postgres Beginner (for example the
// instrumented reducer DB) into the reducer's narrow service-materialization
// transaction surface, so the lineage writer can commit its supersede + insert +
// snapshot writes atomically over the shared instrumented connection.
type ServiceMaterializationBeginner struct {
	Beginner Beginner
}

// BeginServiceMaterializationTx opens a transaction wrapped in the reducer's
// lineage-writer surface.
func (b ServiceMaterializationBeginner) BeginServiceMaterializationTx(
	ctx context.Context,
) (reducer.ServiceMaterializationTx, error) {
	tx, err := b.Beginner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return serviceMaterializationTx{tx: tx}, nil
}

type serviceMaterializationTx struct {
	tx Transaction
}

func (t serviceMaterializationTx) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	return t.tx.ExecContext(ctx, query, args...)
}

// QueryRowContext runs the single-row supersede query over the transaction and
// returns a row whose Scan reads the first result (or reports sql.ErrNoRows when
// the service had no prior active generation).
func (t serviceMaterializationTx) QueryRowContext(
	ctx context.Context,
	query string,
	args ...any,
) reducer.ServiceMaterializationRow {
	rows, err := t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return serviceMaterializationRow{err: err}
	}
	return serviceMaterializationRow{rows: rows}
}

func (t serviceMaterializationTx) Commit() error   { return t.tx.Commit() }
func (t serviceMaterializationTx) Rollback() error { return t.tx.Rollback() }

type serviceMaterializationRow struct {
	rows Rows
	err  error
}

func (r serviceMaterializationRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	defer func() { _ = r.rows.Close() }()
	if !r.rows.Next() {
		if err := r.rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	if err := r.rows.Scan(dest...); err != nil {
		return err
	}
	return r.rows.Err()
}
