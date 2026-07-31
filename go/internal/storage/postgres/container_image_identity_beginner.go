// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// ContainerImageIdentityBeginner adapts a Postgres transaction beginner into
// the reducer identity writer's publication-and-legacy-cleanup surface.
type ContainerImageIdentityBeginner struct {
	Beginner Beginner
}

// BeginContainerImageIdentityTx opens one atomic identity write transaction.
func (b ContainerImageIdentityBeginner) BeginContainerImageIdentityTx(
	ctx context.Context,
) (reducer.ContainerImageIdentityTransaction, error) {
	tx, err := b.Beginner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return containerImageIdentityTx{tx: tx}, nil
}

type containerImageIdentityTx struct {
	tx Transaction
}

func (tx containerImageIdentityTx) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	return tx.tx.ExecContext(ctx, query, args...)
}

func (tx containerImageIdentityTx) ExecContainerImageIdentityClaimed(
	ctx context.Context,
	query string,
	args ...any,
) (int, bool, error) {
	return execContainerImageIdentityClaimed(ctx, tx.tx, query, args...)
}

func (tx containerImageIdentityTx) Commit() error {
	return tx.tx.Commit()
}

func (tx containerImageIdentityTx) Rollback() error {
	return tx.tx.Rollback()
}
