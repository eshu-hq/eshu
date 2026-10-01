// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/boundederr"
)

// guardedReaderError has the shape of the runtime reader pool's private error
// (#7482): a fixed public text that names no relation, with the driver error
// kept behind Unwrap.
type guardedReaderError struct{ cause error }

func (e guardedReaderError) Error() string { return "PostgreSQL reader query failed" }
func (e guardedReaderError) Unwrap() error { return e.cause }

// TestContentReferenceIndexUnavailableClassifiesByTypeNotText pins the #7253
// interaction: the guarded reader pool (#7482) and the bounded writer pool give
// every driver error a fixed text, so the missing-table check that lets a
// cross-repo reference search fall back to the content scan must read the
// SQLSTATE, not the message.
func TestContentReferenceIndexUnavailableClassifiesByTypeNotText(t *testing.T) {
	t.Parallel()

	missingTable := &pgconn.PgError{
		Severity: "ERROR", Code: "42P01",
		Message: `relation "content_file_references" does not exist`,
	}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"raw missing table", missingTable, true},
		{"bounded missing table", boundederr.Wrap(missingTable), true},
		{"guarded reader missing table", guardedReaderError{cause: missingTable}, true},
		{"guarded reader unrelated failure", guardedReaderError{cause: errors.New("boom")}, false},
		{"bounded missing table, wrapped by the caller", errors.Join(errors.New("check"), boundederr.Wrap(missingTable)), true},
		{"a different missing relation", boundederr.Wrap(&pgconn.PgError{Code: "42P01", Message: `relation "other_table" does not exist`}), false},
		{"same relation, other SQLSTATE", boundederr.Wrap(&pgconn.PgError{Code: "23505", Message: `relation "content_file_references" does not exist`}), false},
		{"bounded unrelated failure", boundederr.Wrap(errors.New("boom")), false},
		{"driver text without a PgError", errors.New(`pq: relation "content_file_references" does not exist`), true},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := contentReferenceIndexUnavailable(tt.err); got != tt.want {
				t.Fatalf("contentReferenceIndexUnavailable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
