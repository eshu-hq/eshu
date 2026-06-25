// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Tenant-isolation tests for governance_audit_events.tenant_id (issue #3717).
//
// These tests MUST fail before the schema + store changes land and pass after.
//
// Design decision: global/NULL-tenant events are NOT visible to a tenant-admin
// caller. Only the shared-operator (no TenantID filter) sees them. This is
// the deliberate choice documented in the issue: "not visible to a tenant
// admin, visible only to global shared-operator".

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// ---------------------------------------------------------------------------
// Schema DDL must include tenant_id + workspace_id columns and ALTER TABLE
// ---------------------------------------------------------------------------

// TestGovernanceAuditSchemaDDLIncludesTenantID asserts the bootstrap DDL
// contains the tenant_id and workspace_id columns and their additive ALTER
// TABLE migrations. Fails before the schema is updated.
func TestGovernanceAuditSchemaDDLIncludesTenantID(t *testing.T) {
	t.Parallel()

	ddl := GovernanceAuditEventsSchemaSQL()
	for _, want := range []string{
		"tenant_id TEXT NULL",
		"ADD COLUMN IF NOT EXISTS tenant_id",
		"workspace_id TEXT NULL",
		"ADD COLUMN IF NOT EXISTS workspace_id",
	} {
		if !strings.Contains(ddl, want) {
			t.Fatalf("governance audit schema missing %q", want)
		}
	}
}

// ---------------------------------------------------------------------------
// buildGovernanceAuditListQuery must include a tenant_id predicate
// ---------------------------------------------------------------------------

// TestGovernanceAuditListQueryIncludesTenantIDPredicate asserts that a query
// with TenantID set emits a tenant_id = $N clause and binds the value.
// Fails before TenantID is added to GovernanceAuditQuery and the builder.
func TestGovernanceAuditListQueryIncludesTenantIDPredicate(t *testing.T) {
	t.Parallel()

	q, args := buildGovernanceAuditListQuery(GovernanceAuditQuery{
		OperatorAuthorized: true,
		TenantID:           "tenant_a",
	})
	if !strings.Contains(q, "tenant_id") {
		t.Fatalf("query missing tenant_id predicate:\n%s", q)
	}
	found := false
	for _, arg := range args {
		if s, ok := arg.(string); ok && s == "tenant_a" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("args %v missing tenant_a value; query:\n%s", args, q)
	}
}

// TestGovernanceAuditListQueryNoTenantHasNoTenantPredicate asserts that an
// unscoped query (global operator) does not add a tenant_id predicate so the
// shared operator sees all events.
func TestGovernanceAuditListQueryNoTenantHasNoTenantPredicate(t *testing.T) {
	t.Parallel()

	q, _ := buildGovernanceAuditListQuery(GovernanceAuditQuery{
		OperatorAuthorized: true,
	})
	// The query should never hard-code a tenant predicate when TenantID is "".
	if strings.Contains(q, "tenant_id = $") {
		t.Fatalf("unscoped query has unexpected tenant_id predicate:\n%s", q)
	}
}

// ---------------------------------------------------------------------------
// Append must persist tenant_id / workspace_id
// ---------------------------------------------------------------------------

// TestGovernanceAuditAppendStoresTenantID asserts that an event carrying a
// TenantID is persisted with that value in the INSERT args.
// Fails before TenantID is added to the Event struct and the INSERT.
func TestGovernanceAuditAppendStoresTenantID(t *testing.T) {
	t.Parallel()

	db := newGovernanceAuditTenantMemoryDB()
	store := NewGovernanceAuditStore(db)

	event := governanceAuditTenantEvent("tenant_xyz", "", governanceAuditTenantTestTime())
	if err := store.Append(context.Background(), []governanceaudit.Event{event}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if len(db.rows) == 0 {
		t.Fatal("no rows stored after Append")
	}
	for _, row := range db.rows {
		if row.tenantID != "tenant_xyz" {
			t.Fatalf("stored tenantID = %q, want %q", row.tenantID, "tenant_xyz")
		}
	}
}

// TestGovernanceAuditAppendStoresWorkspaceID asserts workspace_id is persisted
// alongside tenant_id.
func TestGovernanceAuditAppendStoresWorkspaceID(t *testing.T) {
	t.Parallel()

	db := newGovernanceAuditTenantMemoryDB()
	store := NewGovernanceAuditStore(db)

	event := governanceAuditTenantEvent("tenant_a", "workspace_a", governanceAuditTenantTestTime())
	if err := store.Append(context.Background(), []governanceaudit.Event{event}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	for _, row := range db.rows {
		if row.workspaceID != "workspace_a" {
			t.Fatalf("stored workspaceID = %q, want %q", row.workspaceID, "workspace_a")
		}
	}
}

// TestGovernanceAuditAppendSystemEventStoresNullTenantID asserts that a genuine
// system/global event (empty TenantID) is persisted with NULL tenant_id, not a
// fabricated tenant.
func TestGovernanceAuditAppendSystemEventStoresNullTenantID(t *testing.T) {
	t.Parallel()

	db := newGovernanceAuditTenantMemoryDB()
	store := NewGovernanceAuditStore(db)

	event := governanceaudit.Event{
		Type:       governanceaudit.EventTypeBootstrap,
		ActorClass: governanceaudit.ActorClassSystem,
		ScopeClass: governanceaudit.ScopeClassAdmin,
		Decision:   governanceaudit.DecisionAllowed,
		ReasonCode: "bootstrap_complete",
		// TenantID intentionally empty — this is a genuine global event.
		OccurredAt: governanceAuditTenantTestTime(),
	}
	if err := store.Append(context.Background(), []governanceaudit.Event{event}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	for _, row := range db.rows {
		if row.tenantID != "" {
			t.Fatalf("system event stored tenantID = %q, want empty/NULL", row.tenantID)
		}
	}
}

// ---------------------------------------------------------------------------
// SummaryForTenant: tenant-scoped aggregate
// ---------------------------------------------------------------------------

// TestGovernanceAuditSummaryScopedToTenant asserts SummaryForTenant includes a
// tenant_id filter in the SQL. Fails before SummaryForTenant is implemented.
func TestGovernanceAuditSummaryScopedToTenant(t *testing.T) {
	t.Parallel()

	now := governanceAuditTenantTestTime()
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{
			{"total", "", int64(2), now},
			{"decision", string(governanceaudit.DecisionAllowed), int64(2), now},
		}}},
	}
	store := NewGovernanceAuditStore(db)

	summary, err := store.SummaryForTenant(context.Background(), "tenant_a")
	if err != nil {
		t.Fatalf("SummaryForTenant error = %v, want nil", err)
	}
	if got, want := summary.Total, 2; got != want {
		t.Fatalf("Total = %d, want %d", got, want)
	}
	if len(db.queries) == 0 {
		t.Fatal("SummaryForTenant issued no queries")
	}
	q := db.queries[0].query
	if !strings.Contains(q, "tenant_id") {
		t.Fatalf("SummaryForTenant query missing tenant_id filter:\n%s", q)
	}
}

// ---------------------------------------------------------------------------
// Cross-tenant List isolation using the memory DB
// ---------------------------------------------------------------------------

// TestGovernanceAuditListCrossTenantIsolation appends events for tenant_a and
// tenant_b then asserts a TenantID-filtered List returns only the matching rows.
// Fails before the TenantID predicate is added to buildGovernanceAuditListQuery.
func TestGovernanceAuditListCrossTenantIsolation(t *testing.T) {
	t.Parallel()

	now := governanceAuditTenantTestTime()
	db := newGovernanceAuditTenantMemoryDB()
	store := NewGovernanceAuditStore(db)

	eventA := governanceAuditTenantEvent("tenant_a", "", now)
	eventB := governanceAuditTenantEvent("tenant_b", "", now.Add(time.Second))
	if err := store.Append(context.Background(), []governanceaudit.Event{eventA}); err != nil {
		t.Fatalf("Append tenant_a: %v", err)
	}
	if err := store.Append(context.Background(), []governanceaudit.Event{eventB}); err != nil {
		t.Fatalf("Append tenant_b: %v", err)
	}

	events, err := store.List(context.Background(), GovernanceAuditQuery{
		OperatorAuthorized: true,
		TenantID:           "tenant_a",
	})
	if err != nil {
		t.Fatalf("List error = %v, want nil", err)
	}
	if len(events) == 0 {
		t.Fatal("List(tenant_a) returned 0 events, expected at least 1")
	}
	for _, ev := range events {
		if ev.TenantID != "tenant_a" {
			t.Fatalf("cross-tenant leak: List(tenant_a) returned event with TenantID=%q", ev.TenantID)
		}
	}
}

// TestGovernanceAuditListGlobalOperatorSeesBothTenants verifies an unscoped
// List (shared operator, no TenantID) returns events from all tenants.
func TestGovernanceAuditListGlobalOperatorSeesBothTenants(t *testing.T) {
	t.Parallel()

	now := governanceAuditTenantTestTime()
	db := newGovernanceAuditTenantMemoryDB()
	store := NewGovernanceAuditStore(db)

	if err := store.Append(context.Background(), []governanceaudit.Event{
		governanceAuditTenantEvent("tenant_a", "", now),
		governanceAuditTenantEvent("tenant_b", "", now.Add(time.Second)),
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	events, err := store.List(context.Background(), GovernanceAuditQuery{
		OperatorAuthorized: true,
	})
	if err != nil {
		t.Fatalf("List error = %v, want nil", err)
	}
	tenants := map[string]bool{}
	for _, ev := range events {
		tenants[ev.TenantID] = true
	}
	if !tenants["tenant_a"] || !tenants["tenant_b"] {
		t.Fatalf("global List got tenants %v, want both tenant_a and tenant_b", tenants)
	}
}

// TestGovernanceAuditListGlobalEventsHiddenFromTenantAdmin asserts that a
// tenant-scoped List does not return global/NULL-tenant events.
func TestGovernanceAuditListGlobalEventsHiddenFromTenantAdmin(t *testing.T) {
	t.Parallel()

	now := governanceAuditTenantTestTime()
	db := newGovernanceAuditTenantMemoryDB()
	store := NewGovernanceAuditStore(db)

	// Global event — TenantID intentionally empty.
	globalEvent := governanceaudit.Event{
		Type:       governanceaudit.EventTypeBootstrap,
		ActorClass: governanceaudit.ActorClassSystem,
		ScopeClass: governanceaudit.ScopeClassAdmin,
		Decision:   governanceaudit.DecisionAllowed,
		ReasonCode: "bootstrap_complete",
		OccurredAt: now,
	}
	if err := store.Append(context.Background(), []governanceaudit.Event{globalEvent}); err != nil {
		t.Fatalf("Append global event: %v", err)
	}

	events, err := store.List(context.Background(), GovernanceAuditQuery{
		OperatorAuthorized: true,
		TenantID:           "tenant_a",
	})
	if err != nil {
		t.Fatalf("List error = %v, want nil", err)
	}
	for _, ev := range events {
		if ev.TenantID == "" {
			t.Fatal("tenant-scoped List returned global/NULL-tenant event — must be hidden from tenant admin")
		}
	}
}

// ---------------------------------------------------------------------------
// Handler-level: tenant admin can read own-tenant audit; shared-op sees all
// ---------------------------------------------------------------------------
// These are tested in the query package (admin_identity_reads_tenant_test.go).
// The store-level tests above are sufficient for this package.

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func governanceAuditTenantTestTime() time.Time {
	return time.Date(2026, time.June, 24, 10, 0, 0, 0, time.UTC)
}

// governanceAuditTenantEvent builds a valid tenant-scoped event.
func governanceAuditTenantEvent(tenantID, workspaceID string, at time.Time) governanceaudit.Event {
	return governanceaudit.Event{
		Type:        governanceaudit.EventTypeReadAuthorization,
		ActorClass:  governanceaudit.ActorClassScopedToken,
		ActorIDHash: "sha256:aaaaaaaaaaaaaaaa",
		ScopeClass:  governanceaudit.ScopeClassTenant,
		ScopeIDHash: "sha256:bbbbbbbbbbbbbbbb",
		Decision:    governanceaudit.DecisionAllowed,
		ReasonCode:  "policy_allowed",
		TenantID:    tenantID,
		WorkspaceID: workspaceID,
		OccurredAt:  at,
	}
}

// governanceAuditTenantMemoryDB is a purpose-built fake ExecQueryer that tracks
// the tenant_id and workspace_id arguments persisted by Append and applies a
// tenant filter in QueryContext to simulate the Postgres WHERE clause.
type governanceAuditTenantMemoryDB struct {
	rows  map[string]governanceAuditTenantMemoryRow
	execs []fakeExecCall
}

type governanceAuditTenantMemoryRow struct {
	eventID     string
	tenantID    string
	workspaceID string
	occurredAt  time.Time
}

func newGovernanceAuditTenantMemoryDB() *governanceAuditTenantMemoryDB {
	return &governanceAuditTenantMemoryDB{rows: map[string]governanceAuditTenantMemoryRow{}}
}

func (db *governanceAuditTenantMemoryDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	db.execs = append(db.execs, fakeExecCall{query: query, args: args})
	if strings.Contains(query, "INSERT INTO governance_audit_events") {
		// After the implementation adds tenant_id and workspace_id the column
		// count becomes governanceAuditColumnsPerRow (updated to 15).
		// Layout: eventID(0)…persistedAt(12), tenantID(13), workspaceID(14).
		n := governanceAuditColumnsPerRow
		for i := 0; i+n <= len(args); i += n {
			row := governanceAuditTenantMemoryRow{
				eventID:    args[i+0].(string),
				occurredAt: args[i+n-3].(time.Time), // persistedAt is at n-1; occurredAt at n-3? no:
				// Actual layout after implementation:
				// 0:eventID 1:eventType 2:actorClass 3:actorIDHash 4:servicePrincipalID
				// 5:scopeClass 6:scopeIDHash 7:decision 8:reasonCode 9:correlationID
				// 10:policyRevisionHash 11:occurredAt 12:persistedAt 13:tenantID 14:workspaceID
				tenantID:    governanceAuditTenantStringArg(args[i+n-2]),
				workspaceID: governanceAuditTenantStringArg(args[i+n-1]),
			}
			row.occurredAt = args[i+11].(time.Time)
			if _, exists := db.rows[row.eventID]; !exists {
				db.rows[row.eventID] = row
			}
		}
		return fakeResult{}, nil
	}
	if strings.Contains(query, "CREATE TABLE") || strings.Contains(query, "CREATE INDEX") ||
		strings.Contains(query, "ALTER TABLE") {
		return fakeResult{}, nil
	}
	return nil, sql.ErrNoRows
}

func (db *governanceAuditTenantMemoryDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	// Detect a tenant filter by scanning for "tenant_id = $N" and finding the
	// corresponding arg value.
	var tenantFilter string
	for i, arg := range args {
		placeholder := fmt.Sprintf("tenant_id = $%d", i+1)
		if strings.Contains(query, placeholder) {
			if s, ok := arg.(string); ok {
				tenantFilter = s
			}
			break
		}
	}

	// Build result rows from in-memory store, applying the tenant filter.
	var rows [][]any
	for _, row := range db.rows {
		if tenantFilter != "" && row.tenantID != tenantFilter {
			continue
		}
		// Also skip global events (empty tenantID) when a tenant filter is set.
		if tenantFilter != "" && row.tenantID == "" {
			continue
		}
		// Columns returned by scanGovernanceAuditEvent (after implementation adds
		// tenant_id and workspace_id at the end):
		rows = append(rows, []any{
			string(governanceaudit.EventTypeReadAuthorization),
			string(governanceaudit.ActorClassScopedToken),
			sql.NullString{String: "sha256:aaaaaaaaaaaaaaaa", Valid: true},
			sql.NullString{},
			string(governanceaudit.ScopeClassTenant),
			sql.NullString{String: "sha256:bbbbbbbbbbbbbbbb", Valid: true},
			string(governanceaudit.DecisionAllowed),
			"policy_allowed",
			sql.NullString{},
			sql.NullString{},
			row.occurredAt,
			sql.NullString{String: row.tenantID, Valid: row.tenantID != ""},
			sql.NullString{String: row.workspaceID, Valid: row.workspaceID != ""},
		})
	}
	return &queueFakeRows{rows: rows}, nil
}

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

	db := newGovernanceAuditTenantMemoryDB()
	store := NewGovernanceAuditStore(db)
	now := governanceAuditTenantTestTime()

	evA := governanceAuditTenantEvent("tenant_a", "", now)
	evB := governanceAuditTenantEvent("tenant_b", "", now)

	if err := store.Append(context.Background(), []governanceaudit.Event{evA, evB}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	if len(db.rows) != 2 {
		t.Fatalf("stored %d rows, want 2: cross-tenant identical-field events must not collide", len(db.rows))
	}
	tenants := map[string]bool{}
	for _, row := range db.rows {
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
