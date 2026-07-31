// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
)

type containerImageIdentityUnitCompletedCutover struct{}

func (containerImageIdentityUnitCompletedCutover) ContainerImageIdentityCutoverExists(
	context.Context,
	string,
	string,
) (bool, error) {
	return true, nil
}

func (containerImageIdentityUnitCompletedCutover) ContainerImageIdentityLegacyCleanupComplete(
	context.Context,
	string,
	string,
) (bool, error) {
	return true, nil
}

type containerImageIdentityUnitClaimedExecer struct {
	db *fakeWorkloadIdentityExecer
}

func (e containerImageIdentityUnitClaimedExecer) ExecContainerImageIdentityClaimed(
	ctx context.Context,
	query string,
	args ...any,
) (int, bool, error) {
	if query != containerImageIdentityCompletedCutoverPublishOnlyQuery {
		return 0, false, fmt.Errorf("unexpected unit completed-cutover query")
	}
	if len(args) != 22 {
		return 0, false, fmt.Errorf("completed-cutover args = %d, want 22", len(args))
	}
	if _, err := e.db.ExecContext(ctx, reducerFactBatchInsertQuery, args[:16]...); err != nil {
		return 0, false, err
	}
	return 0, true, nil
}

func newContainerImageIdentityUnitWriter(
	db *fakeWorkloadIdentityExecer,
) PostgresContainerImageIdentityWriter {
	cutover := containerImageIdentityUnitCompletedCutover{}
	return PostgresContainerImageIdentityWriter{
		DB:                  db,
		CutoverLookup:       cutover,
		LegacyCleanupLookup: cutover,
		ClaimedExecer:       containerImageIdentityUnitClaimedExecer{db: db},
	}
}
