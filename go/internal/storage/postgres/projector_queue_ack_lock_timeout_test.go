// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// scopeLockErrDB fails the Ack scope update with scopeErr and records every
// statement, so Ack's error mapping can be checked without a database.
type scopeLockErrDB struct {
	recordingExecQueryer
	scopeErr error
}

func (d *scopeLockErrDB) Begin(context.Context) (db.Transaction, error) {
	d.beginCalls++
	return scopeLockErrTx{parent: d}, nil
}

type scopeLockErrTx struct{ parent *scopeLockErrDB }

func (tx scopeLockErrTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	result, err := tx.parent.ExecContext(ctx, query, args...)
	if strings.Contains(query, "UPDATE ingestion_scopes") && tx.parent.scopeErr != nil {
		return nil, tx.parent.scopeErr
	}
	return result, err
}

func (tx scopeLockErrTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return tx.parent.QueryContext(ctx, query, args...)
}

func (scopeLockErrTx) Commit() error   { return nil }
func (scopeLockErrTx) Rollback() error { return nil }

func ackWorkForLockTimeoutTest() projector.ScopeGenerationWork {
	return projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: "scope-1"},
		Generation:   scope.ScopeGeneration{GenerationID: "generation-1"},
		AttemptCount: 1,
	}
}

func TestProjectorAckMapsOnlyLockTimeoutToDeferral(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		code         string
		wantDeferred bool
	}{
		{name: "lock_timeout", code: "55P03", wantDeferred: true},
		{name: "deadlock", code: "40P01", wantDeferred: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &scopeLockErrDB{scopeErr: &pgconn.PgError{Code: tt.code}}
			queue := NewProjectorQueue(fake, "projector-1", time.Minute)

			err := queue.Ack(context.Background(), ackWorkForLockTimeoutTest(), projector.Result{})
			if err == nil {
				t.Fatal("Ack() error = nil, want the scope update error")
			}
			if got := errors.Is(err, projector.ErrWorkAckDeferred); got != tt.wantDeferred {
				t.Fatalf("errors.Is(err, ErrWorkAckDeferred) = %v, want %v (err = %v)", got, tt.wantDeferred, err)
			}
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != tt.code {
				t.Fatalf("Ack() error = %v, want the original SQLSTATE %s preserved", err, tt.code)
			}
		})
	}
}

func TestProjectorAckLockTimeoutIsPostgresUnitsBelowAckBudget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config time.Duration
		want   string
	}{
		{name: "default", config: 0, want: "2000ms"},
		{name: "sub-second", config: 1500 * time.Millisecond, want: "1500ms"},
		// Go formats 90s as "1m30s", which PostgreSQL rejects; it also exceeds
		// the projector's 5 s Ack budget, so it is clamped.
		{name: "clamped", config: 90 * time.Second, want: "4000ms"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &scopeLockErrDB{}
			queue := NewProjectorQueue(fake, "projector-1", time.Minute)
			queue.AckScopeLockTimeout = tt.config

			_ = queue.Ack(context.Background(), ackWorkForLockTimeoutTest(), projector.Result{})
			if len(fake.execs) == 0 || !strings.Contains(fake.execs[0].query, "set_config('lock_timeout'") {
				t.Fatalf("first Ack statement = %v, want lock_timeout set_config", fake.execs)
			}
			if got := fake.execs[0].args[0]; got != tt.want {
				t.Fatalf("lock_timeout = %v, want %s", got, tt.want)
			}
		})
	}
}
