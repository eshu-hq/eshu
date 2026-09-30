// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package advisory

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type rejectingEvidenceReader struct {
	calls int
	err   error
}

func (r *rejectingEvidenceReader) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	r.calls++
	return nil, r.err
}

func TestAdvisoryEvidenceGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &rejectingEvidenceReader{err: want}
	store := NewPostgresEvidenceStoreWithReadStore(guard)
	_, err := store.ListAdvisoryEvidence(t.Context(), EvidenceFilter{CVEID: "CVE-2026-1234", Limit: 1})
	if !errors.Is(err, want) || guard.calls != 1 {
		t.Fatalf("error %v, guarded queries %d; want stale reader and one query", err, guard.calls)
	}
}
