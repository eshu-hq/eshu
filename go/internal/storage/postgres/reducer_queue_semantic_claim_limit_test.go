// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestReducerQueueSemanticClaimLimitDefaultDisablesCrossScopeCap(t *testing.T) {
	t.Parallel()

	queue := ReducerQueue{RequireProjectorDrainBeforeClaim: true}
	if got := queue.semanticEntityClaimLimit(); got != 0 {
		t.Fatalf("semanticEntityClaimLimit() = %d, want 0", got)
	}
}

func TestReducerQueueClaimBypassesSemanticGlobalCapWhenDisabled(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 13, 1, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	queue := ReducerQueue{
		db:                               db,
		LeaseOwner:                       "semantic-worker",
		LeaseDuration:                    time.Minute,
		Now:                              func() time.Time { return now },
		RequireProjectorDrainBeforeClaim: true,
	}

	_, claimed, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if claimed {
		t.Fatal("Claim() claimed = true, want false from empty rows")
	}
	if got, want := db.queries[0].args[6], 0; got != want {
		t.Fatalf("semantic claim limit arg = %v, want %v", got, want)
	}
	if !strings.Contains(db.queries[0].query, "domain <> 'semantic_entity_materialization' OR $7 <= 0 OR (") {
		t.Fatalf("claim query does not bypass semantic cap when disabled:\n%s", db.queries[0].query)
	}
}

func TestReducerQueueClaimBatchBypassesSemanticGlobalCapWhenDisabled(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 13, 1, 5, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	queue := ReducerQueue{
		db:                               db,
		LeaseOwner:                       "semantic-worker",
		LeaseDuration:                    time.Minute,
		Now:                              func() time.Time { return now },
		RequireProjectorDrainBeforeClaim: true,
	}

	intents, err := queue.ClaimBatch(context.Background(), 8)
	if err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}
	if len(intents) != 0 {
		t.Fatalf("ClaimBatch() returned %d intents from empty rows, want 0", len(intents))
	}
	if got, want := db.queries[0].args[6], 0; got != want {
		t.Fatalf("semantic claim limit arg = %v, want %v", got, want)
	}
	if !strings.Contains(db.queries[0].query, "domain <> 'semantic_entity_materialization' OR $7 <= 0 OR (") {
		t.Fatalf("batch claim query does not bypass semantic cap when disabled:\n%s", db.queries[0].query)
	}
}
