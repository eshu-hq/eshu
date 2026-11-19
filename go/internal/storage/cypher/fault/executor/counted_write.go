// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifafaultinjection

package executor

import (
	"context"
	"errors"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// errFaultingExecutorInnerNoCountedWrite is the counted-write counterpart of
// errFaultingExecutorInnerNoGroup (#10938).
var errFaultingExecutorInnerNoCountedWrite = errors.New("faulting executor: inner executor does not support counted writes")

// ExecuteCountedWrite applies the same fail-graph-write-once-then-succeed
// fault as Execute before delegating to inner (#10938): a counted write is a
// mutating write, so the scripted fault vocabulary covers it identically. It
// fails closed with errFaultingExecutorInnerNoCountedWrite when inner does
// not implement cypher.CountedWriteExecutor.
func (fe *FaultingExecutor) ExecuteCountedWrite(ctx context.Context, stmt cypher.Statement) (int64, error) {
	cw, ok := fe.inner.(cypher.CountedWriteExecutor)
	if !ok {
		return 0, errors.Join(errFaultingExecutorInnerNoCountedWrite, cypher.ErrCountedWriteUnsupported)
	}
	ordinal := int(fe.callOrdinal.Add(1))
	if err := fe.maybeHoldBeforeDelegation(ctx, ordinal, []cypher.Statement{stmt}); err != nil {
		return 0, err
	}
	armedCtx, err := fe.maybeFailOnce(ctx, ordinal, []cypher.Statement{stmt}, true)
	if err != nil {
		return 0, err
	}
	return cw.ExecuteCountedWrite(armedCtx, stmt)
}

// ExecuteCountedWriteGroup applies the same fail-graph-write-once-then-succeed
// fault as ExecuteGroup before delegating to inner (#10938), including the
// restart-backend-between-phase-groups surface on the same execute_group
// marker. It fails closed with errFaultingExecutorInnerNoCountedWrite when
// inner does not implement cypher.CountedWriteGroupExecutor.
func (fe *FaultingExecutor) ExecuteCountedWriteGroup(ctx context.Context, stmts []cypher.Statement) ([]int64, error) {
	cw, ok := fe.inner.(cypher.CountedWriteGroupExecutor)
	if !ok {
		return nil, errors.Join(errFaultingExecutorInnerNoCountedWrite, cypher.ErrCountedWriteUnsupported)
	}
	ordinal := int(fe.callOrdinal.Add(1))
	if err := fe.maybeHoldBeforeDelegation(ctx, ordinal, stmts); err != nil {
		return nil, err
	}
	armedCtx, err := fe.maybeFailOnce(ctx, ordinal, stmts, true)
	if err != nil {
		return nil, err
	}
	counts, err := cw.ExecuteCountedWriteGroup(armedCtx, stmts)
	if err != nil {
		return nil, err
	}
	return counts, fe.maybeRestartAfterGroup(
		ctx,
		int(fe.groupOrdinal.Add(1)),
		restartSurfaceExecuteGroup,
		stmts,
	)
}
