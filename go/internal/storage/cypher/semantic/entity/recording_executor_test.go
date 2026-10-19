// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// recordingExecutor and recordingGroupExecutor duplicate the recording fakes
// from package cypher (writer_test.go, recording_group_executor_test.go).
// Test fakes cannot be imported across the package split, so each side
// carries its own copy. The copies stay minimal: only the members the entity
// leaf tests exercise (recorded Execute/ExecuteGroup calls).

type recordingExecutor struct {
	calls []sourcecypher.Statement
}

func (r *recordingExecutor) Execute(_ context.Context, stmt sourcecypher.Statement) error {
	r.calls = append(r.calls, stmt)
	return nil
}

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

// dispatchRouteRecorder duplicates the dispatch-route fake from package cypher
// (code_evidence_retract_dispatch_test.go). Test fakes cannot be imported
// across the package split, so each side carries its own copy.
type dispatchRouteRecorder struct {
	executeCyphers []string
	groupCyphers   []string
}

func (r *dispatchRouteRecorder) Execute(_ context.Context, statement sourcecypher.Statement) error {
	r.executeCyphers = append(r.executeCyphers, statement.Cypher)
	return nil
}

func (r *dispatchRouteRecorder) ExecuteGroup(_ context.Context, statements []sourcecypher.Statement) error {
	for _, statement := range statements {
		r.groupCyphers = append(r.groupCyphers, statement.Cypher)
	}
	return nil
}
