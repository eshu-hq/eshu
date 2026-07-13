// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func TestNornicDBPhaseGroupExecutorWrapsChunkFailureDetails(t *testing.T) {
	t.Parallel()

	inner := &recordingGroupChunkExecutor{
		failAtCall: 2,
		err:        errors.New("context canceled"),
	}
	executor := nornicDBPhaseGroupExecutor{
		Inner:         inner,
		MaxStatements: 2,
	}

	stmts := []sourcecypher.Statement{
		{Cypher: "RETURN 1"},
		{Cypher: "RETURN 2"},
		{Cypher: "RETURN 3"},
	}

	err := executor.ExecutePhaseGroup(context.Background(), stmts)
	if err == nil {
		t.Fatal("ExecutePhaseGroup() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "phase-group chunk 2/2") {
		t.Fatalf("ExecutePhaseGroup() error = %q, want chunk ordinal context", err.Error())
	}
	if !strings.Contains(err.Error(), "statements 3-3 of 3") {
		t.Fatalf("ExecutePhaseGroup() error = %q, want statement range context", err.Error())
	}
	if !strings.Contains(err.Error(), `first_statement="RETURN 3"`) {
		t.Fatalf("ExecutePhaseGroup() error = %q, want first statement summary", err.Error())
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("ExecutePhaseGroup() error = %q, want inner error context", err.Error())
	}
}
