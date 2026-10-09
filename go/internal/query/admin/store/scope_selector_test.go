// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestResolveScopeID pins the shared skip/reopen resolver contract (#7732):
// zero matches are not-found, one match resolves, two matches fail closed
// naming both scopes.
func TestResolveScopeID(t *testing.T) {
	t.Parallel()

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		s := &postgresStore{database: &scriptedScopeQueryer{ids: nil}}
		if _, err := s.resolveScopeID(context.Background(), "nope"); !errors.Is(err, admin.ErrScopeSelectorNotFound) {
			t.Fatalf("resolveScopeID() error = %v, want ErrScopeSelectorNotFound", err)
		}
	})

	t.Run("one match resolves", func(t *testing.T) {
		t.Parallel()
		s := &postgresStore{database: &scriptedScopeQueryer{ids: []string{"scope-a"}}}
		got, err := s.resolveScopeID(context.Background(), "  scope-a ")
		if err != nil {
			t.Fatalf("resolveScopeID() error = %v", err)
		}
		if got != "scope-a" {
			t.Fatalf("resolveScopeID() = %q, want scope-a", got)
		}
	})

	t.Run("two matches fail closed", func(t *testing.T) {
		t.Parallel()
		s := &postgresStore{database: &scriptedScopeQueryer{ids: []string{"scope-a", "scope-b"}}}
		_, err := s.resolveScopeID(context.Background(), "scope-a")
		var ambiguous admin.ScopeSelectorAmbiguousError
		if !errors.As(err, &ambiguous) {
			t.Fatalf("resolveScopeID() error = %v, want ScopeSelectorAmbiguousError", err)
		}
		if ambiguous.Selector != "scope-a" || len(ambiguous.ScopeIDs) != 2 ||
			ambiguous.ScopeIDs[0] != "scope-a" || ambiguous.ScopeIDs[1] != "scope-b" {
			t.Fatalf("ambiguous = %+v, want selector scope-a with [scope-a scope-b]", ambiguous)
		}
	})
}

// scriptedScopeQueryer answers the resolve SELECT with a fixed id list.
type scriptedScopeQueryer struct {
	ids []string
}

func (database *scriptedScopeQueryer) QueryContext(_ context.Context, _ string, _ ...any) (db.Rows, error) {
	return &scopeIDRows{ids: database.ids}, nil
}

func (*scriptedScopeQueryer) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	return nil, fmt.Errorf("unexpected ExecContext call")
}

// scopeIDRows yields each id once, in order.
type scopeIDRows struct {
	ids []string
	pos int
}

func (rows *scopeIDRows) Next() bool {
	return rows.pos < len(rows.ids)
}

func (rows *scopeIDRows) Scan(dest ...any) error {
	if len(dest) != 1 {
		return fmt.Errorf("scopeIDRows scans one column, got %d destinations", len(dest))
	}
	id, ok := dest[0].(*string)
	if !ok {
		return fmt.Errorf("scopeIDRows scans into *string, got %T", dest[0])
	}
	*id = rows.ids[rows.pos]
	rows.pos++
	return nil
}

func (*scopeIDRows) Err() error   { return nil }
func (*scopeIDRows) Close() error { return nil }
