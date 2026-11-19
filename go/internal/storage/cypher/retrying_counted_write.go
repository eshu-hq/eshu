// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

// ExecuteCountedWrite delegates to Inner, retrying on transient graph write
// errors with the same budget and cadence as Execute (#10938). A counted
// write has no cheap correct fallback — an unverified batch fails closed —
// so unlike the read-only probes it retries. The count is captured only from
// the successful attempt; failed attempts report nothing.
func (r *RetryingExecutor) ExecuteCountedWrite(ctx context.Context, stmt Statement) (int64, error) {
	cw, ok := r.Inner.(CountedWriteExecutor)
	if !ok {
		return 0, fmt.Errorf("inner executor does not support ExecuteCountedWrite: %w", ErrCountedWriteUnsupported)
	}
	var count int64
	err := r.runWithRetry(
		ctx,
		string(stmt.Operation),
		func() error {
			c, err := cw.ExecuteCountedWrite(ctx, stmt)
			if err == nil {
				count = c
			}
			return err
		},
		func(err error) string { return classifyRetryableGraphWriteError(err, stmt) },
		nil,
		[]Statement{stmt},
	)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// ExecuteCountedWriteGroup delegates to Inner.ExecuteCountedWriteGroup with
// the same retry budget and cadence as ExecuteGroup (#10938). The group is
// atomic: counts are captured only when the whole group commits, so a retried
// group never mixes counts across attempts.
func (r *RetryingExecutor) ExecuteCountedWriteGroup(ctx context.Context, stmts []Statement) ([]int64, error) {
	cw, ok := r.Inner.(CountedWriteGroupExecutor)
	if !ok {
		return nil, fmt.Errorf("inner executor does not support ExecuteCountedWriteGroup: %w", ErrCountedWriteUnsupported)
	}
	var counts []int64
	err := r.runWithRetry(
		ctx,
		groupOperationLabel(stmts),
		func() error {
			c, err := cw.ExecuteCountedWriteGroup(ctx, stmts)
			if err == nil {
				counts = c
			}
			return err
		},
		func(err error) string { return classifyRetryableGraphWriteGroupError(err, stmts) },
		stmts,
		stmts,
	)
	if err != nil {
		return nil, err
	}
	return counts, nil
}
