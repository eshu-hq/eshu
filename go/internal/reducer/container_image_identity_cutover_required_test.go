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
		1,
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
	if got, want := len(tx.queries), 3; got != want {
		t.Fatalf("transaction queries = %d, want admission plus marker fence plus publication", got)
	}
	if tx.queries[0] != containerImageIdentityAdmissionQuery {
		t.Fatalf("first transaction query = %q, want the admission CAS (#5874)", tx.queries[0])
	}
	if tx.queries[1] != containerImageIdentityCutoverFenceQuery {
		t.Fatalf("second transaction query = %q, want cutover fence", tx.queries[1])
	}
	if tx.queries[2] != containerImageIdentityPublishAndLegacyCleanupQuery {
		t.Fatalf("third transaction query = %q, want atomic publication", tx.queries[2])
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("transaction committed=%t rolledBack=%t, want true/false", tx.committed, tx.rolledBack)
	}
}

// TestContainerImageIdentityWriterDoesNothingForFullyHeldDemotion proves a
// pass with nothing to publish or clean up (rows and LegacyFactIDs both
// empty) still issues exactly the container_image_identity_write_admission
// CAS, and nothing else -- no transaction, no publication. This is the
// zero-decision / negative-observation case #5874 exists to fence: without
// the admission CAS, a fully-negative pass would leave no durable trace that
// it looked, so a genuinely stale later pass could stay unopposed.
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
		FencingToken: 1,
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
	if len(db.execs) != 1 {
		t.Fatalf("database statements = %d, want 1 (the admission CAS only)", len(db.execs))
	}
	if db.execs[0].query != containerImageIdentityAdmissionQuery {
		t.Fatalf("database statement = %q, want the admission CAS", db.execs[0].query)
	}
}
