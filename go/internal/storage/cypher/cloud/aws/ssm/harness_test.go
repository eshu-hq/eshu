// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ssm

import (
	"context"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// The fakes below are leaf-local copies of the sibling leaves' test doubles.
// Test fakes cannot be imported across the package split, so each side
// carries its own copy; keep the copies behavior-identical.

// recordingExecutor captures every Execute statement for shape assertions.
type recordingExecutor struct {
	statements []sourcecypher.Statement
}

func (r *recordingExecutor) Execute(_ context.Context, statement sourcecypher.Statement) error {
	r.statements = append(r.statements, statement)
	return nil
}

// recordingGroupExecutor additionally records ExecuteGroup calls so tests
// can prove retracts never run grouped.
type recordingGroupExecutor struct {
	recordingExecutor
	groupCalls int
}

func (r *recordingGroupExecutor) ExecuteGroup(_ context.Context, stmts []sourcecypher.Statement) error {
	r.groupCalls++
	r.statements = append(r.statements, stmts...)
	return nil
}

// stubCommittedReader scripts one read result for committed-capture and
// stale-count queries, recording every issued query for shape assertions.
type stubCommittedReader struct {
	rows    []map[string]any
	err     error
	queries []string
}

func (s *stubCommittedReader) Run(_ context.Context, query string, _ map[string]any) ([]map[string]any, error) {
	s.queries = append(s.queries, query)
	if s.err != nil {
		return nil, s.err
	}
	return s.rows, nil
}
