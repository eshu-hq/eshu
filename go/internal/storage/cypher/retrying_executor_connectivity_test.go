// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type typedErrorExecutor struct {
	calls   atomic.Int32
	failFor int
	err     error
}

func (e *typedErrorExecutor) Execute(context.Context, Statement) error {
	n := int(e.calls.Add(1))
	if n <= e.failFor {
		return e.err
	}
	return nil
}

func TestRetryingExecutorRetriesDriverConnectivityError(t *testing.T) {
	t.Parallel()

	inner := &typedErrorExecutor{
		failFor: 1,
		err: &neo4jdriver.ConnectivityError{
			Inner: errors.New("dial tcp 172.20.9.185:7687: connect: connection refused"),
		},
	}
	retrying := &RetryingExecutor{
		Inner:      inner,
		MaxRetries: 2,
		BaseDelay:  1 * time.Millisecond,
	}

	err := retrying.Execute(context.Background(), Statement{Operation: OperationCanonicalUpsert})
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil after retry", err)
	}
	if got, want := int(inner.calls.Load()), 2; got != want {
		t.Fatalf("calls = %d, want %d", got, want)
	}
}

func TestRetryingExecutorConnectivityErrorExhaustionRemainsQueueRetryable(t *testing.T) {
	t.Parallel()

	inner := &typedErrorExecutor{
		failFor: 10,
		err: &neo4jdriver.ConnectivityError{
			Inner: errors.New("EOF"),
		},
	}
	retrying := &RetryingExecutor{
		Inner:      inner,
		MaxRetries: 2,
		BaseDelay:  1 * time.Millisecond,
	}

	err := retrying.Execute(context.Background(), Statement{Operation: OperationCanonicalUpsert})
	if err == nil {
		t.Fatal("Execute() error = nil, want retry exhaustion error")
	}
	var retryable interface{ Retryable() bool }
	if !errors.As(err, &retryable) || !retryable.Retryable() {
		t.Fatalf("Execute() error retryable = false, want true: %v", err)
	}
	if got, want := int(inner.calls.Load()), 3; got != want {
		t.Fatalf("calls = %d, want %d", got, want)
	}
}
