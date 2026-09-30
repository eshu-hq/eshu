// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package drift

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type rejectingDriftReader struct {
	calls int
	err   error
}

func (r *rejectingDriftReader) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	r.calls++
	return nil, r.err
}

func TestTerraformDriftQueryOnlyReadPort(t *testing.T) {
	want := errors.New("reader unavailable")
	reader := &rejectingDriftReader{err: want}
	store := NewPostgresFindingStoreWithReadStore(reader)
	filter := FindingFilter{ScopeID: "state_snapshot:s3:hash-1", Limit: 1}
	_, listErr := store.ListActiveFindings(t.Context(), filter)
	_, countErr := store.CountActiveFindings(t.Context(), filter)
	if !errors.Is(listErr, want) || !errors.Is(countErr, want) || reader.calls != 2 {
		t.Fatalf("list error %v, count error %v, calls %d; want reader error twice", listErr, countErr, reader.calls)
	}
}
