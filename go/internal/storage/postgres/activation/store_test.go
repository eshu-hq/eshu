// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// refusingDatabase fails the test if any statement reaches the database.
type refusingDatabase struct{ t *testing.T }

func (r refusingDatabase) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	r.t.Fatal("argument validation must refuse before any query")
	return nil, nil
}

func (r refusingDatabase) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	r.t.Fatal("argument validation must refuse before any exec")
	return nil, nil
}

func (r refusingDatabase) Begin(context.Context) (db.Transaction, error) {
	r.t.Fatal("argument validation must refuse before any transaction")
	return nil, nil
}

func TestStoreRefusesInvalidArgumentsBeforeTouchingTheDatabase(t *testing.T) {
	t.Parallel()
	store := NewStore(refusingDatabase{t: t})
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"claim blank owner":    func() error { _, err := store.Claim(ctx, "  ", time.Minute); return err },
		"claim zero lease":     func() error { _, err := store.Claim(ctx, "owner", 0); return err },
		"catch up zero page":   func() error { _, err := store.CatchUp(ctx, "", 0); return err },
		"prune zero limit":     func() error { _, err := store.Prune(ctx, time.Hour, 0); return err },
		"prune negative keep":  func() error { _, err := store.Prune(ctx, -time.Second, 1); return err },
		"catch up negative pg": func() error { _, err := store.CatchUp(ctx, "", -1); return err },
	} {
		if err := call(); err == nil {
			t.Errorf("%s: error = nil, want refusal", name)
		}
	}
}

// recordingExecutor captures the one statement Insert runs.
type recordingExecutor struct {
	query string
	args  []any
	err   error
}

func (r *recordingExecutor) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	r.query, r.args = query, args
	return nil, r.err
}

func TestInsertWritesOneIdempotentRowForTheExactGeneration(t *testing.T) {
	t.Parallel()
	exec := &recordingExecutor{}
	if err := Insert(context.Background(), exec, "scope-a", "gen-a", "projector_scope-a_gen-a"); err != nil {
		t.Fatalf("Insert() = %v", err)
	}
	for _, want := range []string{
		"INSERT INTO activation_obligations (scope_id, generation_id, work_item_id)",
		"ON CONFLICT (scope_id, generation_id) DO NOTHING",
	} {
		if !strings.Contains(exec.query, want) {
			t.Fatalf("Insert query missing %q:\n%s", want, exec.query)
		}
	}
	if len(exec.args) != 3 || exec.args[0] != "scope-a" || exec.args[1] != "gen-a" ||
		exec.args[2] != "projector_scope-a_gen-a" {
		t.Fatalf("Insert args = %v", exec.args)
	}
	exec.err = errors.New("boom")
	if err := Insert(context.Background(), exec, "s", "g", "w"); !errors.Is(err, exec.err) {
		t.Fatalf("Insert() error = %v, want wrapped cause", err)
	}
}
