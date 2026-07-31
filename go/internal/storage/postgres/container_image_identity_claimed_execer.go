// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
)

// ContainerImageIdentityClaimedExecer adapts the shared Postgres query surface
// to reducer statements that must return an exact-claim verdict and cleanup
// count from the same database statement.
type ContainerImageIdentityClaimedExecer struct {
	DB Queryer
}

// ExecContainerImageIdentityClaimed runs one exact-claim statement and reports
// whether it returned the locked claim row.
func (e ContainerImageIdentityClaimedExecer) ExecContainerImageIdentityClaimed(
	ctx context.Context,
	query string,
	args ...any,
) (int, bool, error) {
	if e.DB == nil {
		return 0, false, fmt.Errorf(
			"container image identity claimed executor database is required",
		)
	}
	return execContainerImageIdentityClaimed(ctx, e.DB, query, args...)
}

func execContainerImageIdentityClaimed(
	ctx context.Context,
	db Queryer,
	query string,
	args ...any,
) (int, bool, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, false, err
		}
		return 0, false, nil
	}
	var deleted int
	if err := rows.Scan(&deleted); err != nil {
		return 0, false, err
	}
	if rows.Next() {
		return 0, false, fmt.Errorf(
			"container image identity claimed statement returned multiple rows",
		)
	}
	if err := rows.Err(); err != nil {
		return 0, false, err
	}
	return deleted, true, nil
}
