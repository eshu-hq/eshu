// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
)

// recordingGroupExecutor duplicates the recording fake from the edge/writer leaf
// (edge/writer/sql_transaction_test.go). Test fakes cannot be imported across
// the package split, so each side carries its own copy.

type recordingGroupExecutor struct {
	executeCalls []Statement
	groupCalls   [][]Statement
}

func (r *recordingGroupExecutor) Execute(_ context.Context, stmt Statement) error {
	r.executeCalls = append(r.executeCalls, stmt)
	return nil
}

func (r *recordingGroupExecutor) ExecuteGroup(_ context.Context, stmts []Statement) error {
	cloned := make([]Statement, len(stmts))
	copy(cloned, stmts)
	r.groupCalls = append(r.groupCalls, cloned)
	return nil
}
