// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type terraformDriftQueryOnly struct{ queryer db.Queryer }

func (q terraformDriftQueryOnly) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return q.queryer.QueryContext(ctx, query, args...)
}

func TestTerraformConfigStateDriftFindingQueryOnlyReader(t *testing.T) {
	fake := &fakeExecQueryer{queryResponses: []queueFakeRows{{}, {rows: [][]any{{4}}}}}
	store := NewTerraformConfigStateDriftFindingReader(terraformDriftQueryOnly{fake})
	filter := TerraformConfigStateDriftFindingFilter{ScopeID: "state_snapshot:s3:hash-1", Limit: 1}
	if _, err := store.ListActiveFindings(t.Context(), filter); err != nil {
		t.Fatalf("ListActiveFindings: %v", err)
	}
	count, err := store.CountActiveFindings(t.Context(), filter)
	if err != nil || count != 4 {
		t.Fatalf("CountActiveFindings = %d, %v; want 4", count, err)
	}
	if len(fake.queries) != 2 || len(fake.execs) != 0 {
		t.Fatalf("queries %d, writes %d; want 2, 0", len(fake.queries), len(fake.execs))
	}
}
