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

// ExecContainerImageIdentityClaimedAdmission runs one exact-claim statement
// that ALSO carries a leading container_image_identity_write_admission CAS
// (#5874, see containerImageIdentityCompletedCutoverAdmissionCTE), and
// reports both the admission verdict and the claim verdict from the same
// statement.
func (e ContainerImageIdentityClaimedExecer) ExecContainerImageIdentityClaimedAdmission(
	ctx context.Context,
	query string,
	args ...any,
) (int, bool, bool, error) {
	if e.DB == nil {
		return 0, false, false, fmt.Errorf(
			"container image identity claimed executor database is required",
		)
	}
	return execContainerImageIdentityClaimedAdmission(ctx, e.DB, query, args...)
}

// execContainerImageIdentityClaimedAdmission mirrors
// execContainerImageIdentityClaimed but scans the SECOND result column the
// admission-aware statements add: whether the woven-in
// container_image_identity_write_admission CAS admitted this pass. A ZERO-ROW
// result still means "claim rejected" exactly as before -- the claim check
// (current_claim) gates the final SELECT's FROM clause, so an invalid claim
// yields no rows regardless of what the admission CAS did.
func execContainerImageIdentityClaimedAdmission(
	ctx context.Context,
	db Queryer,
	query string,
	args ...any,
) (int, bool, bool, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, false, false, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, false, false, err
		}
		return 0, false, false, nil
	}
	var deleted int
	var admitted bool
	if err := rows.Scan(&deleted, &admitted); err != nil {
		return 0, false, false, err
	}
	if rows.Next() {
		return 0, false, false, fmt.Errorf(
			"container image identity claimed admission statement returned multiple rows",
		)
	}
	if err := rows.Err(); err != nil {
		return 0, false, false, err
	}
	return deleted, admitted, true, nil
}
