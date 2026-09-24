// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package auditstore

// Event-id uniqueness tests for governance_audit_events.tenant_id (issue
// #3717), split out of tenant_test.go to keep both files under the repo's
// 500-line cap. They share tenant_test.go's fixtures
// (governanceAuditTenantEvent, governanceAuditTenantTestTime,
// governanceAuditTenantMemoryDB) from this same package.

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// ---------------------------------------------------------------------------
// Event ID uniqueness: cross-tenant events must not collide
// ---------------------------------------------------------------------------

// TestGovernanceAuditEventIDDistinctAcrossTenants is a regression test for the
// data-loss bug where governanceAuditEventID did not include tenant_id /
// workspace_id. Two events identical in every audit field except TenantID
// produced the same event_id, so the second ON CONFLICT(event_id) DO NOTHING
// silently dropped it. After the fix, the two IDs must be distinct.
func TestGovernanceAuditEventIDDistinctAcrossTenants(t *testing.T) {
	t.Parallel()

	base := governanceAuditTenantEvent("", "", governanceAuditTenantTestTime())
	eventA := base
	eventA.TenantID = "tenant_a"
	eventB := base
	eventB.TenantID = "tenant_b"

	idA := governanceAuditEventID(eventA)
	idB := governanceAuditEventID(eventB)

	if idA == idB {
		t.Fatalf("cross-tenant event_id collision: both tenant_a and tenant_b produced %q — "+
			"the second Append would be silently dropped by ON CONFLICT DO NOTHING", idA)
	}
}

// TestGovernanceAuditEventIDStableWithinTenant asserts that exact-retry
// deduplication still works: two calls to Append with the same event fields AND
// the same tenant produce the same event_id, so the retry is idempotent.
func TestGovernanceAuditEventIDStableWithinTenant(t *testing.T) {
	t.Parallel()

	ev := governanceAuditTenantEvent("tenant_a", "workspace_a", governanceAuditTenantTestTime())
	id1 := governanceAuditEventID(ev)
	id2 := governanceAuditEventID(ev)
	if id1 != id2 {
		t.Fatalf("event_id not stable: same inputs produced %q then %q", id1, id2)
	}
}

// TestGovernanceAuditAppendBothTenantsPersistedDistinctly appends two events
// identical in audit fields but differing in TenantID and asserts both are
// stored (neither is dropped by ON CONFLICT DO NOTHING).
func TestGovernanceAuditAppendBothTenantsPersistedDistinctly(t *testing.T) {
	t.Parallel()

	database := newGovernanceAuditTenantMemoryDB()
	store := NewGovernanceAuditStore(database)
	now := governanceAuditTenantTestTime()

	evA := governanceAuditTenantEvent("tenant_a", "", now)
	evB := governanceAuditTenantEvent("tenant_b", "", now)

	if err := store.Append(context.Background(), []governanceaudit.Event{evA, evB}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	if len(database.rows) != 2 {
		t.Fatalf("stored %d rows, want 2: cross-tenant identical-field events must not collide", len(database.rows))
	}
	tenants := map[string]bool{}
	for _, row := range database.rows {
		tenants[row.tenantID] = true
	}
	if !tenants["tenant_a"] || !tenants["tenant_b"] {
		t.Fatalf("stored tenants %v, want both tenant_a and tenant_b", tenants)
	}
}

func governanceAuditTenantStringArg(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}
