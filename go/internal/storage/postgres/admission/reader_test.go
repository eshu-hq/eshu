// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admissionstore

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type rejectingAdmissionReader struct {
	calls int
	err   error
}

func (r *rejectingAdmissionReader) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	r.calls++
	return nil, r.err
}

func TestAdmissionDecisionReaderUsesQueryOnlyPort(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &rejectingAdmissionReader{err: want}
	reader := NewAdmissionDecisionReader(guard)
	_, err := reader.ListDecisions(context.Background(), AdmissionDecisionFilter{
		Domain: "test", ScopeID: "scope", GenerationID: "generation", Limit: 1,
	})
	if !errors.Is(err, want) || guard.calls != 1 {
		t.Fatalf("read error = %v, guarded queries = %d; want stale reader and one query", err, guard.calls)
	}
}
