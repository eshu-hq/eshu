// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// #7008: when the dispatch/scoped deadline fires mid-read, the topic reader
// must return the scored rows gathered so far with an explicit
// deadline-truncated marker instead of failing the whole call.

// deadlinePartialTopicRows serves a fixed prefix of scored rows, then reports
// the iteration error a cancelled PostgreSQL read surfaces through Rows.Err.
type deadlinePartialTopicRows struct {
	kinds []string
	index int
	err   error
}

func (r *deadlinePartialTopicRows) Next() bool {
	if r.index >= len(r.kinds) {
		return false
	}
	r.index++
	return true
}

func (r *deadlinePartialTopicRows) Scan(dest ...any) error {
	kind := r.kinds[r.index-1]
	entityID, entityName, entityType := "", "", ""
	if kind == "entity" {
		entityID, entityName, entityType = "entity-auth", "resolveAuth", "Function"
	}
	strings := []string{kind, "repo-1", "auth.go", entityID, entityName, entityType, "go", "auth"}
	ints := []int{44, 88, 1}
	si, ii := 0, 0
	for _, target := range dest {
		switch ptr := target.(type) {
		case *string:
			*ptr = strings[si]
			si++
		case *int:
			*ptr = ints[ii]
			ii++
		case *bool:
			*ptr = false
		default:
			return errors.New("deadlinePartialTopicRows: unexpected scan target")
		}
	}
	return nil
}

func (r *deadlinePartialTopicRows) Err() error   { return r.err }
func (r *deadlinePartialTopicRows) Close() error { return nil }

// deadlinePartialTopicStore answers every topic query with a fixed row prefix
// (or a query-time failure) through the guarded read-store seam.
type deadlinePartialTopicStore struct {
	db.ReadStore
	kinds    []string
	err      error
	queryErr error
}

func (s deadlinePartialTopicStore) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	if s.queryErr != nil {
		return nil, s.queryErr
	}
	return &deadlinePartialTopicRows{kinds: s.kinds, err: s.err}, nil
}

func deadlinePartialTopicRequest() CodeTopicInvestigationRequest {
	return CodeTopicInvestigationRequest{
		RepoID:               "repo-1",
		AllowedRepositoryIDs: []string{"repo-1"},
		Terms:                []string{"repo", "sync", "auth"},
		Limit:                26,
		Offset:               0,
	}
}

func TestContentReaderInvestigateCodeTopicDeadlineReturnsBoundedPartial(t *testing.T) {
	t.Parallel()

	reader := NewContentReaderWithReadStore(deadlinePartialTopicStore{
		kinds: []string{"entity", "file"},
		err:   context.DeadlineExceeded,
	})
	rows, err := reader.InvestigateCodeTopic(context.Background(), deadlinePartialTopicRequest())
	if !errors.Is(err, codequery.ErrCodeTopicDeadlineTruncated) {
		t.Fatalf("InvestigateCodeTopic() error = %v, want ErrCodeTopicDeadlineTruncated", err)
	}
	if got, want := len(rows), 2; got != want {
		t.Fatalf("len(rows) = %d, want %d scored rows gathered before the deadline", got, want)
	}
	if got, want := rows[0].SourceKind, "entity"; got != want {
		t.Fatalf("rows[0].SourceKind = %q, want %q (score-ordered prefix)", got, want)
	}
}

// TestContentReaderInvestigateCodeTopicWrappedDeadlineIsMarkedPartial pins
// the pgx driver shape: pgconn maps a client-side deadline to
// errTimeout{err: ctx.Err()} (pgconn/errors.go normalizeTimeoutError, Unwrap),
// so rows.Err() carries a WRAPPED context.DeadlineExceeded, never the bare
// sentinel. The branch must match through errors.Is, not ==.
func TestContentReaderInvestigateCodeTopicWrappedDeadlineIsMarkedPartial(t *testing.T) {
	t.Parallel()

	reader := NewContentReaderWithReadStore(deadlinePartialTopicStore{
		kinds: []string{"entity", "file"},
		err:   fmt.Errorf("timeout: %w", context.DeadlineExceeded),
	})
	rows, err := reader.InvestigateCodeTopic(context.Background(), deadlinePartialTopicRequest())
	if !errors.Is(err, codequery.ErrCodeTopicDeadlineTruncated) {
		t.Fatalf("InvestigateCodeTopic() error = %v, want ErrCodeTopicDeadlineTruncated through the pgx wrap", err)
	}
	if got, want := len(rows), 2; got != want {
		t.Fatalf("len(rows) = %d, want %d scored rows gathered before the deadline", got, want)
	}
}

func TestContentReaderInvestigateCodeTopicQueryDeadlineIsMarkedPartial(t *testing.T) {
	t.Parallel()

	reader := NewContentReaderWithReadStore(deadlinePartialTopicStore{
		queryErr: context.DeadlineExceeded,
	})
	rows, err := reader.InvestigateCodeTopic(context.Background(), deadlinePartialTopicRequest())
	if !errors.Is(err, codequery.ErrCodeTopicDeadlineTruncated) {
		t.Fatalf("InvestigateCodeTopic() error = %v, want ErrCodeTopicDeadlineTruncated", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want empty bounded partial", len(rows))
	}
}

func TestContentReaderInvestigateCodeTopicCanceledStaysErrorOnly(t *testing.T) {
	t.Parallel()

	reader := NewContentReaderWithReadStore(deadlinePartialTopicStore{
		kinds: []string{"entity"},
		err:   context.Canceled,
	})
	_, err := reader.InvestigateCodeTopic(context.Background(), deadlinePartialTopicRequest())
	if err == nil {
		t.Fatal("InvestigateCodeTopic() error = nil, want the cancellation")
	}
	if errors.Is(err, codequery.ErrCodeTopicDeadlineTruncated) {
		t.Fatalf("InvestigateCodeTopic() error = %v, want no deadline-partial marker on cancel", err)
	}
	// codeTopicRows drops rows for every non-deadline error, so the handler
	// stays fail-closed; the reader keeps its results-plus-error contract.
}

func TestContentReaderInvestigateCodeTopicRowsFaultStaysErrorOnly(t *testing.T) {
	t.Parallel()

	boom := errors.New("connection reset mid result")
	reader := NewContentReaderWithReadStore(deadlinePartialTopicStore{
		kinds: []string{"entity"},
		err:   boom,
	})
	_, err := reader.InvestigateCodeTopic(context.Background(), deadlinePartialTopicRequest())
	if !errors.Is(err, boom) {
		t.Fatalf("InvestigateCodeTopic() error = %v, want the iteration fault", err)
	}
	if errors.Is(err, codequery.ErrCodeTopicDeadlineTruncated) {
		t.Fatalf("InvestigateCodeTopic() error = %v, want no deadline-partial marker on fault", err)
	}
	// codeTopicRows drops rows for every non-deadline error, so the handler
	// stays fail-closed; the reader keeps its results-plus-error contract.
}
