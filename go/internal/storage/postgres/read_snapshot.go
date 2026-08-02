// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func withReadOnlyRepeatableRead(
	ctx context.Context,
	db ExecQueryer,
	load func(Queryer) error,
) (err error) {
	beginner, ok := db.(ReadOnlyRepeatableReadBeginner)
	if !ok {
		return fmt.Errorf("database does not support read-only repeatable-read transactions")
	}
	tx, err := beginner.BeginReadOnlyRepeatableRead(ctx)
	if err != nil {
		return fmt.Errorf("begin read-only repeatable-read transaction: %w", err)
	}
	if tx == nil {
		return fmt.Errorf("begin read-only repeatable-read transaction: nil transaction")
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("rollback read-only repeatable-read transaction: %w", rollbackErr))
		}
	}()

	if err := load(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit read-only repeatable-read transaction: %w", err)
	}
	committed = true
	return nil
}
