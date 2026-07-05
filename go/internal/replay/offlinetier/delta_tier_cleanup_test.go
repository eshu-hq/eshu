// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier_test

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

type deltaCleanupExecutor interface {
	Execute(context.Context, cypher.Statement) error
}

type recordingDeltaCleanupExecutor struct {
	calls []cypher.Statement
}

func (r *recordingDeltaCleanupExecutor) Execute(_ context.Context, stmt cypher.Statement) error {
	r.calls = append(r.calls, stmt)
	return nil
}

func TestCleanupDeltaScopeDeletesDeltaFileNodes(t *testing.T) {
	exec := &recordingDeltaCleanupExecutor{}

	cleanupDeltaScope(context.Background(), t, exec)

	for _, call := range exec.calls {
		if strings.Contains(call.Cypher, "MATCH (f:File") &&
			strings.Contains(call.Cypher, "DETACH DELETE f") {
			if got := call.Parameters["repo_id"]; got != deltaRepoID {
				t.Fatalf("File cleanup repo_id = %v, want %q", got, deltaRepoID)
			}
			if got := call.Parameters["repo_path_prefix"]; got != deltaRepoPath+"/" {
				t.Fatalf("File cleanup repo_path_prefix = %v, want %q", got, deltaRepoPath+"/")
			}
			return
		}
	}
	t.Fatalf("cleanupDeltaScope did not delete delta File nodes; calls=%v", exec.calls)
}
