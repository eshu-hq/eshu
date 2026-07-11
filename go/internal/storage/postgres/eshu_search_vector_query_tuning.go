// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
)

const disableJITForSearchVectorDocumentQuerySQL = `SET LOCAL jit = off`

// searchVectorDocumentRows keeps the query-local transaction open until the
// caller finishes consuming rows. Commit closes the cursor before committing;
// Rollback is safe to defer after either a successful commit or an error.
type searchVectorDocumentRows struct {
	Rows
	tx     Transaction
	closed bool
}

func beginSearchVectorDocumentQuery(
	ctx context.Context,
	db ExecQueryer,
	query string,
	args ...any,
) (*searchVectorDocumentRows, error) {
	beginner, ok := db.(Beginner)
	if !ok {
		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		return &searchVectorDocumentRows{Rows: rows}, nil
	}

	tx, err := beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin search vector document read: %w", err)
	}
	if _, err := tx.ExecContext(ctx, disableJITForSearchVectorDocumentQuerySQL); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("disable jit for search vector document read: %w", err)
	}
	queryer := ExecQueryer(tx)
	if instrumented, ok := db.(*InstrumentedDB); ok {
		queryer = &InstrumentedDB{
			Inner: tx, Tracer: instrumented.Tracer,
			Instruments: instrumented.Instruments, StoreName: instrumented.StoreName,
		}
	}
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return &searchVectorDocumentRows{Rows: rows, tx: tx}, nil
}

// Commit completes the query-local transaction after closing the row cursor.
func (r *searchVectorDocumentRows) Commit() error {
	if r.closed {
		return nil
	}
	if err := r.Close(); err != nil {
		_ = r.rollbackTransaction()
		return err
	}
	r.closed = true
	if r.tx == nil {
		return nil
	}
	if err := r.tx.Commit(); err != nil {
		_ = r.tx.Rollback()
		return err
	}
	return nil
}

// Rollback closes the row cursor and releases any open query-local transaction.
func (r *searchVectorDocumentRows) Rollback() {
	if r.closed {
		return
	}
	_ = r.Close()
	r.closed = true
	_ = r.rollbackTransaction()
}

func (r *searchVectorDocumentRows) rollbackTransaction() error {
	if r.tx == nil {
		return nil
	}
	return r.tx.Rollback()
}
