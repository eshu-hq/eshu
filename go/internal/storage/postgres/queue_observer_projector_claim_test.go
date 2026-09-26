// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestQueueObserverStoreProjectorScopesWithMultipleLiveLeases pins the #7115
// invariant query: projector scopes holding more than one unexpired
// claimed/running lease at the observer's clock.
func TestQueueObserverStoreProjectorScopesWithMultipleLiveLeases(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{{int64(3)}}}}}
	observer := NewQueueObserverStore(queryer)
	observer.Now = func() time.Time { return now }

	got, err := observer.ProjectorScopesWithMultipleLiveLeases(context.Background())
	if err != nil {
		t.Fatalf("ProjectorScopesWithMultipleLiveLeases() error = %v", err)
	}
	if got != 3 {
		t.Fatalf("ProjectorScopesWithMultipleLiveLeases() = %d, want 3", got)
	}
	query := queryer.queries[0]
	for _, want := range []string{
		"stage = 'projector'",
		"status IN ('claimed', 'running')",
		"claim_until > $1",
		"GROUP BY scope_id",
		"HAVING COUNT(*) > 1",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("invariant query missing %q:\n%s", want, query)
		}
	}
	if gotNow, ok := queryer.args[0][0].(time.Time); !ok || !gotNow.Equal(now) {
		t.Fatalf("invariant query $1 = %v, want observer clock %v", queryer.args[0], now)
	}
}

// TestQueueObserverStoreProjectorScopesMissingClaimFence pins the #7115
// missing-fence query: scopes with claimable projector work and no
// projector_scope_claim_fences row, which the claim can never take.
func TestQueueObserverStoreProjectorScopesMissingClaimFence(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{{int64(2)}}}}}
	observer := NewQueueObserverStore(queryer)

	got, err := observer.ProjectorScopesMissingClaimFence(context.Background())
	if err != nil {
		t.Fatalf("ProjectorScopesMissingClaimFence() error = %v", err)
	}
	if got != 2 {
		t.Fatalf("ProjectorScopesMissingClaimFence() = %d, want 2", got)
	}
	query := queryer.queries[0]
	for _, want := range []string{
		"COUNT(DISTINCT work.scope_id)",
		"work.stage = 'projector'",
		"work.status IN ('pending', 'retrying', 'claimed', 'running')",
		"NOT EXISTS (",
		"FROM projector_scope_claim_fences AS fence",
		"fence.scope_id = work.scope_id",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("missing-fence query missing %q:\n%s", want, query)
		}
	}
}
