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
	if len(args) != 26 {
		return 0, false, fmt.Errorf("completed-cutover args = %d, want 26", len(args))
	}
	if _, err := e.db.ExecContext(ctx, reducerFactBatchInsertQuery, args[:16]...); err != nil {
		return 0, false, err
	}
	return 0, true, nil
}

// ExecContainerImageIdentityClaimedAdmission is the method the writer's
// completed-cutover path actually calls (#5874); the OLD 3-return method
// above stays only to satisfy ContainerImageIdentityClaimedExecer's other
// requirement. This fake predates the admission CAS and its owning tests
// exercise the surrounding legacy-cleanup control flow, not admission itself,
// so admitted is unconditionally true here -- matching the previous
// always-succeeds behavior.
func (e containerImageIdentityUnitClaimedExecer) ExecContainerImageIdentityClaimedAdmission(
	ctx context.Context,
	query string,
	args ...any,
) (int, bool, bool, error) {
	if query != containerImageIdentityCompletedCutoverPublishOnlyQuery {
		return 0, false, false, fmt.Errorf("unexpected unit completed-cutover query")
	}
	if len(args) != 26 {
		return 0, false, false, fmt.Errorf("completed-cutover args = %d, want 26", len(args))
	}
	if _, err := e.db.ExecContext(ctx, reducerFactBatchInsertQuery, args[:16]...); err != nil {
		return 0, false, false, err
	}
	return 0, true, true, nil
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
