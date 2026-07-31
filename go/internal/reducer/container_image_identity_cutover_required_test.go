// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"
)

func TestContainerImageIdentityWriterFencesPublicationWithoutLegacyCleanup(t *testing.T) {
	t.Parallel()

	write := containerImageIdentityFenceWrite(
		time.Date(2026, time.July, 30, 21, 0, 0, 0, time.UTC),
		ContainerImageIdentityExactDigest,
	)
	write.ClaimEpoch = 1
	tx := &containerImageIdentityRetireTx{}
	beginner := &containerImageIdentityRetireBeginner{tx: tx}
	writer := PostgresContainerImageIdentityWriter{
		DB:       &containerImageIdentityRetireOutsideDB{},
		Beginner: beginner,
	}

	if _, err := writer.WriteContainerImageIdentityDecisions(context.Background(), write); err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v", err)
	}
	if got, want := beginner.calls, 1; got != want {
		t.Fatalf("transaction begin calls = %d, want %d for v2 publication", got, want)
	}
	if got, want := len(tx.queries), 2; got != want {
		t.Fatalf("transaction queries = %d, want marker fence plus publication", got)
	}
	if tx.queries[0] != containerImageIdentityCutoverFenceQuery {
		t.Fatalf("first transaction query = %q, want cutover fence", tx.queries[0])
	}
	if tx.queries[1] != containerImageIdentityPublishAndLegacyCleanupQuery {
		t.Fatalf("second transaction query = %q, want atomic publication", tx.queries[1])
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("transaction committed=%t rolledBack=%t, want true/false", tx.committed, tx.rolledBack)
	}
}

func TestContainerImageIdentityWriterDoesNothingForFullyHeldDemotion(t *testing.T) {
	t.Parallel()

	db := &fakeWorkloadIdentityExecer{}
	beginner := &containerImageIdentityRetireBeginner{tx: &containerImageIdentityRetireTx{}}
	writer := PostgresContainerImageIdentityWriter{DB: db, Beginner: beginner}
	write := ContainerImageIdentityWrite{
		IntentID:     "intent-5854-held",
		ScopeID:      "repository:synthetic",
		GenerationID: "generation-5854-held",
		SourceSystem: "git",
		EvidenceAsOf: time.Date(2026, time.July, 30, 21, 1, 0, 0, time.UTC),
		Decisions: []ContainerImageIdentityDecision{{
			ImageRef: "registry.example.com/team/api:held",
			Outcome:  ContainerImageIdentityUnresolved,
		}},
	}

	if _, err := writer.WriteContainerImageIdentityDecisions(context.Background(), write); err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v", err)
	}
	if beginner.calls != 0 {
		t.Fatalf("transaction begin calls = %d, want 0 for no publication or cleanup", beginner.calls)
	}
	if len(db.execs) != 0 {
		t.Fatalf("database statements = %d, want 0 for no publication or cleanup", len(db.execs))
	}
}
