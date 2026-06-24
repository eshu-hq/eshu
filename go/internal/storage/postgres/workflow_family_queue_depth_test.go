// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
)

// familyQueueExecQueryer adapts the query-only fakeQueryer to the ExecQueryer
// surface NewWorkflowControlStore requires; the store's read path only uses
// QueryContext, so ExecContext is a no-op.
type familyQueueExecQueryer struct {
	*fakeQueryer
}

func (familyQueueExecQueryer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}

func TestWorkflowControlStoreFamilyQueueDepthsGroupsByFamilyAndStatus(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{
		responses: []fakeRows{
			{
				rows: [][]any{
					{"git", "github", "pending", int64(3)},
					{"git", "github", "claimed", int64(1)},
					{"git", "gitlab", "pending", int64(2)},
					{"aws", "aws", "pending", int64(7)},
					{"aws", "aws", "failed_retryable", int64(2)},
					{"aws", "aws", "expired", int64(1)},
				},
			},
		},
	}

	store := NewWorkflowControlStore(familyQueueExecQueryer{queryer})
	depths, err := store.WorkflowFamilyQueueDepths(context.Background())
	if err != nil {
		t.Fatalf("WorkflowFamilyQueueDepths() error = %v", err)
	}

	if got := depths["git"]["github"]["pending"]; got != 3 {
		t.Fatalf("git/github pending = %d, want 3", got)
	}
	if got := depths["git"]["github"]["claimed"]; got != 1 {
		t.Fatalf("git/github claimed = %d, want 1", got)
	}
	if got := depths["git"]["gitlab"]["pending"]; got != 2 {
		t.Fatalf("git/gitlab pending = %d, want 2", got)
	}
	if got := depths["aws"]["aws"]["failed_retryable"]; got != 2 {
		t.Fatalf("aws/aws failed_retryable = %d, want 2", got)
	}
	if got := depths["aws"]["aws"]["expired"]; got != 1 {
		t.Fatalf("aws/aws expired = %d, want 1", got)
	}
	// The query must scope to outstanding statuses only.
	if len(queryer.queries) != 1 || queryer.queries[0] != workflowFamilyQueueDepthQuery {
		t.Fatalf("unexpected queries = %v", queryer.queries)
	}
}
