// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"context"
	"errors"
	"fmt"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// ExecuteCountedWrite forwards one counted write to inner and records the
// statement exactly like Execute (#10938): the matched count flows back to
// the caller, while the capture keeps the statement, error, and Bolt
// counters. The record stays digest-neutral (writes never digest rows), so
// capture mode cannot skew a comparison.
func (e differentialGroupRecorder) ExecuteCountedWrite(ctx context.Context, stmt sourcecypher.Statement) (int64, error) {
	cw, ok := e.inner.(sourcecypher.CountedWriteExecutor)
	if !ok {
		return 0, errors.Join(errDifferentialNoExecuteCountedWrite, sourcecypher.ErrCountedWriteUnsupported)
	}
	var count int64
	err := e.recorded(ctx, stmt, func(call context.Context) error {
		c, err := cw.ExecuteCountedWrite(call, stmt)
		if err != nil {
			return fmt.Errorf("inner counted write: %w", err)
		}
		count = c
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("differential recorded counted write: %w", err)
	}
	return count, nil
}

// ExecuteCountedWriteGroup forwards grouped counted writes to inner and
// records every statement exactly like ExecuteGroup (#10938). Counts are
// captured only when the whole group commits, mirroring the atomicity the
// caller relies on.
func (e differentialGroupRecorder) ExecuteCountedWriteGroup(ctx context.Context, stmts []sourcecypher.Statement) ([]int64, error) {
	cw, ok := e.inner.(sourcecypher.CountedWriteGroupExecutor)
	if !ok {
		return nil, errors.Join(errDifferentialNoExecuteCountedWrite, sourcecypher.ErrCountedWriteUnsupported)
	}
	var counts []int64
	err := e.recordedAll(ctx, stmts, func(call context.Context) error {
		c, err := cw.ExecuteCountedWriteGroup(call, stmts)
		if err != nil {
			return fmt.Errorf("inner counted write group: %w", err)
		}
		counts = c
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("differential recorded counted write group: %w", err)
	}
	return counts, nil
}
