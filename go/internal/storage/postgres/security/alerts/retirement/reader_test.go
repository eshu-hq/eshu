// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retirement

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type scopeRows struct {
	ids  []string
	next int
	err  error
}

func (r *scopeRows) Next() bool { r.next++; return r.next <= len(r.ids) }
func (r *scopeRows) Scan(dest ...any) error {
	*dest[0].(*string) = r.ids[r.next-1]
	return nil
}
func (r *scopeRows) Err() error   { return r.err }
func (r *scopeRows) Close() error { return nil }

type scopeQueryer struct {
	rows  *scopeRows
	err   error
	query string
	args  []any
	calls int
}

func (q *scopeQueryer) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	q.calls++
	q.query, q.args = query, args
	if q.err != nil {
		return nil, q.err
	}
	return q.rows, nil
}

func TestReaderReturnsTheCandidateScopes(t *testing.T) {
	t.Parallel()

	queryer := &scopeQueryer{rows: &scopeRows{ids: []string{"base", "base#shard-1"}}}
	got, err := NewReader(queryer).RetirementCandidates(context.Background(), []string{"base"}, []string{"base#shard-0"}, 10)
	if err != nil || !slices.Equal(got, []string{"base", "base#shard-1"}) {
		t.Fatalf("RetirementCandidates() = %v, %v", got, err)
	}
	for _, want := range []string{
		"scope.parent_scope_id = ANY($1::text[])",
		"scope.scope_id = ANY($1::text[])",
		"scope.scope_id <> ALL($2::text[])",
		"scope.collector_kind = 'security_alert'",
		"scope.active_generation_id IS NOT NULL",
		"state.repositories_total <> 0",
		"LIMIT $3",
	} {
		if !strings.Contains(queryer.query, want) {
			t.Errorf("candidate query is missing %q", want)
		}
	}
	if len(queryer.args) != 3 || queryer.args[2] != 10 {
		t.Errorf("args = %v, want bases, planned, and the limit", queryer.args)
	}
}

func TestReaderSkipsTheReadWithoutBases(t *testing.T) {
	t.Parallel()

	queryer := &scopeQueryer{}
	got, err := NewReader(queryer).RetirementCandidates(context.Background(), nil, []string{"x"}, 10)
	if err != nil || len(got) != 0 || queryer.calls != 0 {
		t.Fatalf("RetirementCandidates() = %v, %v after %d queries; want nothing and no query", got, err, queryer.calls)
	}
}

func TestReaderRefusesANonPositiveLimit(t *testing.T) {
	t.Parallel()

	if _, err := NewReader(&scopeQueryer{}).RetirementCandidates(context.Background(), []string{"b"}, nil, 0); err == nil {
		t.Fatal("limit 0 accepted, want an error")
	}
}

func TestReaderReturnsQueryAndRowErrors(t *testing.T) {
	t.Parallel()

	boom := errors.New("connection reset")
	if _, err := NewReader(&scopeQueryer{err: boom}).RetirementCandidates(context.Background(), []string{"b"}, nil, 1); !errors.Is(err, boom) {
		t.Fatalf("query error = %v, want it wrapped", err)
	}
	rowErr := &scopeQueryer{rows: &scopeRows{err: sql.ErrConnDone}}
	if _, err := NewReader(rowErr).RetirementCandidates(context.Background(), []string{"b"}, nil, 1); !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("rows error = %v, want it wrapped", err)
	}
}
