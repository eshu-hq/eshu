// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// nornicDBRepositoryCommitUniqueConflictMessage is the exact NornicDB
// commit-time UNIQUE conflict body observed in the #7304 CI failure of
// TestLiveRepositoryFirstCreationMergeRaceKeepsOneNode (live-backend job,
// NornicDB leg): 50 concurrent first-creation MERGEs of one Repository id can
// fail one loser's commit with this shape instead of converging in place.
const nornicDBRepositoryCommitUniqueConflictMessage = "commit failed: constraint violation: " +
	"Constraint violation (UNIQUE on Repository.[id]): " +
	"Node with id=repository:wd7285-000-race-1 already exists (uid=0)."

// alwaysFailsWithGroupError is a GroupExecutor stub that returns the same
// error on every call, so a test can prove classification (does the
// production retry chain ever give up and hand back a Retryable error) without
// needing convergence.
type alwaysFailsWithGroupError struct {
	calls atomic.Int32
	err   error
}

func (e *alwaysFailsWithGroupError) Execute(context.Context, cypher.Statement) error {
	e.calls.Add(1)
	return e.err
}

func (e *alwaysFailsWithGroupError) ExecuteGroup(context.Context, []cypher.Statement) error {
	e.calls.Add(1)
	return e.err
}

// TestRepositoryFirstCreationMergeRaceErrorIsClassifiedRetryable is a hermetic
// (non-live) unit proof for the retry contract
// TestLiveRepositoryFirstCreationMergeRaceKeepsOneNode now depends on: the
// exact NornicDB commit-time UNIQUE conflict error string captured from the
// #7304 CI failure must be classified retryable by the same production chain
// that test wraps its writes in -- the persistent *cypher.RetryingExecutor
// every canonical/reducer writer runs through, followed by
// failure.IsRetryable, the projector's live retry-decision authority
// (internal/projector/failure/dead_letter_triage.go). Without this, the live
// test's classification check would either always fail closed (masking the
// real regression class) or silently stop verifying anything.
func TestRepositoryFirstCreationMergeRaceErrorIsClassifiedRetryable(t *testing.T) {
	conflictErr := &neo4jdriver.Neo4jError{
		Code: "Neo.ClientError.Transaction.TransactionCommitFailed",
		Msg:  nornicDBRepositoryCommitUniqueConflictMessage,
	}
	inner := &alwaysFailsWithGroupError{err: conflictErr}
	retrying := &cypher.RetryingExecutor{Inner: inner, MaxRetries: 2, BaseDelay: time.Millisecond}

	err := retrying.ExecuteGroup(context.Background(), []cypher.Statement{{
		Cypher: cypher.CanonicalRepoDependencyUpsertCypher,
	}})

	if err == nil {
		t.Fatal("ExecuteGroup = nil, want the stub's persistent conflict error (it never succeeds)")
	}
	if got := inner.calls.Load(); got != 3 { // initial attempt + 2 configured retries
		t.Fatalf("inner ExecuteGroup calls = %d, want 3 (RetryingExecutor did not replay the MERGE in-process)", got)
	}
	if !failure.IsRetryable(err) {
		t.Fatalf("failure.IsRetryable(%v) = false, want true: the exact #7304 NornicDB commit-time UNIQUE "+
			"conflict message must be classified retryable so the projector requeues instead of dead-lettering", err)
	}
}
