// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/reducer/containerimage"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// ContainerImageIdentityClaimedExecer adapts the shared Postgres query surface
// to reducer statements that must return an exact-claim verdict and cleanup
// count from the same database statement.
type ContainerImageIdentityClaimedExecer struct {
	DB db.Queryer
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

// ExecContainerImageIdentitySupportPublish runs the digest-v3 support-set
// publish statement and reports the legacy cleanup count, the claim verdict,
// and the previous-last_set_id comparison (#8284) from the same statement.
// The publish RETURNING carries (deleted_count, support_changed); statements
// through the shared claimed method above keep their single-column shape.
func (e ContainerImageIdentityClaimedExecer) ExecContainerImageIdentitySupportPublish(
	ctx context.Context,
	query string,
	args ...any,
) (containerimage.ContainerImageIdentitySupportPublishResult, error) {
	if e.DB == nil {
		return containerimage.ContainerImageIdentitySupportPublishResult{}, fmt.Errorf(
			"container image identity claimed executor database is required",
		)
	}
	rows, err := e.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return containerimage.ContainerImageIdentitySupportPublishResult{}, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return containerimage.ContainerImageIdentitySupportPublishResult{}, err
		}
		return containerimage.ContainerImageIdentitySupportPublishResult{}, nil
	}
	var publish containerimage.ContainerImageIdentitySupportPublishResult
	if err := rows.Scan(&publish.LegacyRowsDeleted, &publish.SupportChanged); err != nil {
		return containerimage.ContainerImageIdentitySupportPublishResult{}, err
	}
	if rows.Next() {
		return containerimage.ContainerImageIdentitySupportPublishResult{}, fmt.Errorf(
			"container image identity support publish statement returned multiple rows",
		)
	}
	if err := rows.Err(); err != nil {
		return containerimage.ContainerImageIdentitySupportPublishResult{}, err
	}
	publish.ClaimValid = true
	return publish, nil
}

func execContainerImageIdentityClaimed(
	ctx context.Context,
	database db.Queryer,
	query string,
	args ...any,
) (int, bool, error) {
	rows, err := database.QueryContext(ctx, query, args...)
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
