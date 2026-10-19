// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationship

import (
	"context"
	"sync/atomic"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// The fakes below are leaf-local copies of the root cypher package's test
// doubles. Test fakes cannot be imported across the package split, so each
// side carries its own copy; keep the copies behavior-identical.

type recordingExecutor struct {
	calls     []sourcecypher.Statement
	errAtCall error

	// readCalls records every Reader.Run invocation (used by shell-exec
	// orphan-cleanup retract tests, the only production path that reads
	// through EdgeWriter.Reader). readCandidates/readConnected script the S1
	// candidate keys and which of those currently have a relationship (S2);
	// both are nil by default, so a test that never exercises shell-exec
	// cleanup reads sees zero candidates and the production code performs no
	// further read or write.
	readCalls      []sourcecypher.Statement
	readCandidates []string
	readConnected  map[string]bool
	readErr        error
}

func (r *recordingExecutor) Execute(_ context.Context, statement sourcecypher.Statement) error {
	r.calls = append(r.calls, statement)
	if r.errAtCall != nil {
		return r.errAtCall
	}

	return nil
}

// Run implements orphan.OrphanSweepReader so recordingExecutor can double
// as the EdgeWriter.Reader in shell-exec orphan-cleanup retract tests. It
// distinguishes the S1 candidate-keys read (no "keys" parameter) from the S2
// connected-keys read (a "keys" parameter) the same way the production
// statements do.
func (r *recordingExecutor) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	r.readCalls = append(r.readCalls, sourcecypher.Statement{Cypher: cypher, Parameters: params})
	if r.readErr != nil {
		return nil, r.readErr
	}
	if keys, ok := params["keys"].([]string); ok {
		rows := make([]map[string]any, 0, len(keys))
		for _, k := range keys {
			if r.readConnected[k] {
				rows = append(rows, map[string]any{"key": k})
			}
		}
		return rows, nil
	}
	rows := make([]map[string]any, 0, len(r.readCandidates))
	for _, k := range r.readCandidates {
		rows = append(rows, map[string]any{"key": k})
	}
	return rows, nil
}

// recordingGroupExecutor duplicates the recording fake from the root cypher
// package (recording_group_executor_test.go). Test fakes cannot be imported
// across the package split, so each side carries its own copy.

type recordingGroupExecutor struct {
	executeCalls []sourcecypher.Statement
	groupCalls   [][]sourcecypher.Statement
}

func (r *recordingGroupExecutor) Execute(_ context.Context, stmt sourcecypher.Statement) error {
	r.executeCalls = append(r.executeCalls, stmt)
	return nil
}

func (r *recordingGroupExecutor) ExecuteGroup(_ context.Context, stmts []sourcecypher.Statement) error {
	cloned := make([]sourcecypher.Statement, len(stmts))
	copy(cloned, stmts)
	r.groupCalls = append(r.groupCalls, cloned)
	return nil
}

// backendRestartTerminalGroupExecutor always fails with err so retry and
// classification tests can drive writers without a backend. Copy of the
// cloud/resource leaf's fake: test fakes cannot be imported across the
// package split, so each side carries its own copy.
type backendRestartTerminalGroupExecutor struct {
	err   error
	calls atomic.Int32
}

func (e *backendRestartTerminalGroupExecutor) Execute(context.Context, sourcecypher.Statement) error {
	e.calls.Add(1)
	return e.err
}

func (e *backendRestartTerminalGroupExecutor) ExecuteGroup(context.Context, []sourcecypher.Statement) error {
	e.calls.Add(1)
	return e.err
}
