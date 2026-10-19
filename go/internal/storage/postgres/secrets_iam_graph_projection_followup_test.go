// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
)

// TestSecretsIAMGraphProjectionFollowUpWriterEnqueuesProjectionIntent proves
// the follow-up writer admits exactly one secrets_iam_graph_projection
// reducer intent for the scope generation whose read models just committed.
// Without this row no live component runs the projection stage, so a
// generation would drain clean while its edges never materialize.
func TestSecretsIAMGraphProjectionFollowUpWriterEnqueuesProjectionIntent(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	writer := NewSecretsIAMGraphProjectionFollowUpWriter(db)
	if err := writer.EnqueueGraphProjectionFollowUp(
		context.Background(), "vault:scope", "gen-1", "secrets_iam_posture",
	); err != nil {
		t.Fatalf("EnqueueGraphProjectionFollowUp() error = %v, want nil", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec calls = %d, want 1", len(db.execs))
	}
	args := db.execs[0].args
	joined := make([]string, 0, len(args))
	for _, arg := range args {
		switch value := arg.(type) {
		case string:
			joined = append(joined, value)
		case []byte:
			joined = append(joined, string(value))
		}
	}
	for _, want := range []string{
		"secrets_iam_graph_projection",
		"vault:scope",
		"gen-1",
		"secrets_iam_graph_projection:vault:scope",
	} {
		found := false
		for _, got := range joined {
			if strings.Contains(got, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("enqueued intent args %q miss %q", joined, want)
		}
	}
}

// TestSecretsIAMGraphProjectionFollowUpWriterRequiresDatabase proves the
// writer fails closed without a database handle instead of silently
// dropping the projection trigger.
func TestSecretsIAMGraphProjectionFollowUpWriterRequiresDatabase(t *testing.T) {
	t.Parallel()

	writer := NewSecretsIAMGraphProjectionFollowUpWriter(nil)
	if err := writer.EnqueueGraphProjectionFollowUp(
		context.Background(), "vault:scope", "gen-1", "secrets_iam_posture",
	); err == nil {
		t.Fatal("EnqueueGraphProjectionFollowUp() with nil database error = nil, want non-nil")
	}
}
