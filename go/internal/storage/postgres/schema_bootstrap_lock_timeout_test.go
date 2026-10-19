// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestSchemaBootstrapCoordinationLockTimeoutDefaults pins the #6738 fix-4
// contract: an explicit per-statement lock_timeout survives withDefaults,
// and an unset one falls back to the 5 s statement bound the bootstrap has
// always applied (not to zero, which would disable lock_timeout entirely,
// and not to either coordination wait).
func TestSchemaBootstrapCoordinationLockTimeoutDefaults(t *testing.T) {
	t.Parallel()
	if got := (schemaBootstrapCoordination{}.withDefaults()).lockTimeout; got != defaultSchemaLockTimeout {
		t.Fatalf("default lockTimeout = %s, want %s", got, defaultSchemaLockTimeout)
	}
	explicit := schemaBootstrapCoordination{lockTimeout: 42 * time.Second}.withDefaults()
	if explicit.lockTimeout != 42*time.Second {
		t.Fatalf("explicit lockTimeout overwritten: %+v", explicit)
	}
}

// lockTimeoutCaptureBootstrap is a lock-owning, migration-tracking executor
// that records the statement bounds the bootstrap would apply instead of
// touching a database, so the test proves the configured lock_timeout
// reaches the per-statement bound without a live Postgres.
type lockTimeoutCaptureBootstrap struct {
	bounds schemaStatementBounds
	calls  int
}

func (f *lockTimeoutCaptureBootstrap) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}

func (f *lockTimeoutCaptureBootstrap) withSchemaBootstrapLock(_ context.Context, _ *slog.Logger, _ time.Duration, apply func(db.Executor) error) error {
	return apply(f)
}

func (f *lockTimeoutCaptureBootstrap) applyTrackedDefinitions(_ context.Context, _ []Definition, bounds schemaStatementBounds, _ *slog.Logger) error {
	f.bounds = bounds
	f.calls++
	return nil
}

// TestApplyBootstrapWithOptionsThreadsLockTimeout proves the full option
// chain: BootstrapOptions.LockTimeout (from ESHU_SCHEMA_LOCK_TIMEOUT)
// becomes the schemaStatementBounds.lockTimeout the tracked migrator
// applies to every statement, and an unset option keeps the default.
func TestApplyBootstrapWithOptionsThreadsLockTimeout(t *testing.T) {
	t.Parallel()
	explicit := &lockTimeoutCaptureBootstrap{}
	if err := ApplyBootstrapWithOptions(context.Background(), explicit, BootstrapOptions{LockTimeout: 42 * time.Second}); err != nil {
		t.Fatalf("ApplyBootstrapWithOptions: %v", err)
	}
	if explicit.calls != 1 || explicit.bounds.lockTimeout != 42*time.Second {
		t.Fatalf("explicit run calls = %d lockTimeout = %s, want 1 call and 42s", explicit.calls, explicit.bounds.lockTimeout)
	}

	unset := &lockTimeoutCaptureBootstrap{}
	if err := ApplyBootstrapWithOptions(context.Background(), unset, BootstrapOptions{}); err != nil {
		t.Fatalf("ApplyBootstrapWithOptions: %v", err)
	}
	if unset.calls != 1 || unset.bounds.lockTimeout != defaultSchemaLockTimeout {
		t.Fatalf("default run calls = %d lockTimeout = %s, want 1 call and %s", unset.calls, unset.bounds.lockTimeout, defaultSchemaLockTimeout)
	}
}
