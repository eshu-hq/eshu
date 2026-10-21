// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kubernetes

import (
	"context"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// recordingExecutor duplicates the recording fake from package cypher
// (writer_test.go). Test fakes cannot be imported across the package split,
// so each side carries its own copy. This copy keeps only the Execute/call
// surface the Kubernetes node-writer tests use; the shell-exec
// orphan-cleanup read scripting (Run) stays in the packages that test reads.
type recordingExecutor struct {
	calls     []sourcecypher.Statement
	errAtCall error
}

func (r *recordingExecutor) Execute(_ context.Context, statement sourcecypher.Statement) error {
	r.calls = append(r.calls, statement)
	if r.errAtCall != nil {
		return r.errAtCall
	}

	return nil
}

// recordingGroupExecutor duplicates the recording fake from package cypher
// (recording_group_executor_test.go) for the same reason.
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

// sqlSequentialRecordingExecutor duplicates the sequential-group fake from
// the edge/writer leaf (sql_retract_test.go) for the same reason: it
// implements GroupExecutor while recording sequential Execute calls
// separately, so the no-group retract-dispatch guard can tell the two
// dispatch paths apart.
type sqlSequentialRecordingExecutor struct {
	calls      []sourcecypher.Statement
	groupCalls [][]sourcecypher.Statement
}

func (r *sqlSequentialRecordingExecutor) Execute(_ context.Context, stmt sourcecypher.Statement) error {
	r.calls = append(r.calls, stmt)
	return nil
}

func (r *sqlSequentialRecordingExecutor) ExecuteGroup(_ context.Context, stmts []sourcecypher.Statement) error {
	cloned := make([]sourcecypher.Statement, len(stmts))
	copy(cloned, stmts)
	r.groupCalls = append(r.groupCalls, cloned)
	return nil
}

// noopGroupExecutor duplicates the no-op fake from the cloud leaves
// (cloud/aws/ec2/harness_test.go) for the same reason.
type noopGroupExecutor struct{}

func (noopGroupExecutor) Execute(context.Context, sourcecypher.Statement) error {
	return nil
}

func (noopGroupExecutor) ExecuteGroup(context.Context, []sourcecypher.Statement) error {
	return nil
}
