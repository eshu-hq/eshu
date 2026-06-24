// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sandbox_test

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/sandbox"
)

func TestNewPostgresReadOnlyExecutor_NonNil(t *testing.T) {
	t.Parallel()

	// NewPostgresReadOnlyExecutor(nil) must return a non-nil Executor without
	// panicking. A nil *sql.DB is valid here because no method is called during
	// construction.
	exec := sandbox.NewPostgresReadOnlyExecutor(nil)
	if exec == nil {
		t.Fatal("NewPostgresReadOnlyExecutor(nil) returned nil, want non-nil Executor")
	}
}

func TestPostgresReadOnlyExecutor_Cypher_NotWired(t *testing.T) {
	t.Parallel()

	// Cypher execution is explicitly not wired in v1: the executor must return
	// an error immediately, before touching the db handle (which is nil here).
	exec := sandbox.NewPostgresReadOnlyExecutor(nil)
	rows, err := exec.Exec(context.Background(), sandbox.DialectCypher, "MATCH (n) RETURN n", sandbox.DefaultCaps())
	if err == nil {
		t.Fatal("Exec(DialectCypher) = nil error, want non-nil")
	}
	if rows != 0 {
		t.Errorf("Exec(DialectCypher) rows = %d, want 0", rows)
	}
	// The error message must communicate that Cypher is not wired in v1.
	msg := err.Error()
	if msg == "" {
		t.Error("Exec(DialectCypher) error message is empty, want descriptive message")
	}
}
