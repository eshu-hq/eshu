// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// fakeCLIAuditAppender captures every governance-audit event appended during
// a test, mirroring go/cmd/api/seed_initial_admin_test.go's fakeAuditAppender
// (cmd/eshu and cmd/api are separate main packages and cannot share test
// helpers any more than they can share production code).
type fakeCLIAuditAppender struct {
	mu     sync.Mutex
	events []governanceaudit.Event
}

func (f *fakeCLIAuditAppender) Append(_ context.Context, events []governanceaudit.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, events...)
	return nil
}

func auditEventsContainReason(events []governanceaudit.Event, reasonCode string) bool {
	for _, e := range events {
		if e.ReasonCode == reasonCode {
			return true
		}
	}
	return false
}

// auditEventsLeakCLI reports whether any event's formatted representation
// contains any of the given plaintext values, mirroring cmd/api's
// auditEventsLeak.
func auditEventsLeakCLI(events []governanceaudit.Event, plaintexts ...string) bool {
	for _, e := range events {
		rendered := fmt.Sprintf("%+v", e)
		for _, p := range plaintexts {
			if p != "" && strings.Contains(rendered, p) {
				return true
			}
		}
	}
	return false
}

// TestAuditBootstrapCredentialRetrievedRecordsEvent proves `eshu admin
// initial-credential` records a durable, bounded retrieval event: retrieval
// is repeatable until the credential's first login consumes it, so who
// pulled it must be recorded (epic #4962 acceptance criterion).
func TestAuditBootstrapCredentialRetrievedRecordsEvent(t *testing.T) {
	appender := &fakeCLIAuditAppender{}
	auditBootstrapCredentialRetrieved(context.Background(), appender, "key-a", nil)

	if !auditEventsContainReason(appender.events, bootstrapCredentialAuditReasonRetrieved) {
		t.Fatalf("no durable audit event for credential retrieval: %#v", appender.events)
	}
	if len(appender.events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(appender.events))
	}
	got := appender.events[0]
	if got.Type != governanceaudit.EventTypeBootstrap {
		t.Fatalf("Type = %q, want %q", got.Type, governanceaudit.EventTypeBootstrap)
	}
	if got.ActorClass != governanceaudit.ActorClassSystem {
		t.Fatalf("ActorClass = %q, want %q", got.ActorClass, governanceaudit.ActorClassSystem)
	}
	if got.Decision != governanceaudit.DecisionAllowed {
		t.Fatalf("Decision = %q, want %q", got.Decision, governanceaudit.DecisionAllowed)
	}
	if got.CorrelationID != "key:key-a" {
		t.Fatalf("CorrelationID = %q, want %q", got.CorrelationID, "key:key-a")
	}
	if got.TenantID == "" || got.WorkspaceID == "" {
		t.Fatalf("TenantID/WorkspaceID unset: %#v", got)
	}
}

// TestAuditBootstrapCredentialRetrievedFailureRecordsDeniedEvent proves a
// failed retrieval attempt (already consumed, wrong DEK) is audited too, not
// only a successful one — mirroring auditBootstrapCredentialGenerated's
// success/failure symmetry in go/cmd/api/seed_initial_admin_audit.go.
func TestAuditBootstrapCredentialRetrievedFailureRecordsDeniedEvent(t *testing.T) {
	appender := &fakeCLIAuditAppender{}
	auditBootstrapCredentialRetrieved(context.Background(), appender, "key-a", fmt.Errorf("decrypt failed"))

	if !auditEventsContainReason(appender.events, bootstrapCredentialAuditReasonRetrieveFailed) {
		t.Fatalf("no durable audit event for failed credential retrieval: %#v", appender.events)
	}
	got := appender.events[0]
	if got.Decision != governanceaudit.DecisionDenied {
		t.Fatalf("Decision = %q, want %q", got.Decision, governanceaudit.DecisionDenied)
	}
	if got.CorrelationID != "key:key-a" {
		t.Fatalf("CorrelationID = %q, want %q (the envelope's key_id is known even when Open fails)", got.CorrelationID, "key:key-a")
	}
}

// TestAuditBootstrapCredentialResetRecordsEvent proves `eshu admin
// reset-initial-credential` records a durable, bounded reset event,
// correlated to the newly sealed envelope's key_id.
func TestAuditBootstrapCredentialResetRecordsEvent(t *testing.T) {
	appender := &fakeCLIAuditAppender{}
	auditBootstrapCredentialReset(context.Background(), appender, "key-b", nil)

	if !auditEventsContainReason(appender.events, bootstrapCredentialAuditReasonReset) {
		t.Fatalf("no durable audit event for credential reset: %#v", appender.events)
	}
	if len(appender.events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(appender.events))
	}
	if got := appender.events[0].CorrelationID; got != "key:key-b" {
		t.Fatalf("CorrelationID = %q, want %q", got, "key:key-b")
	}
}

// TestAuditBootstrapCredentialResetFailureRecordsDeniedEvent proves a failed
// reset attempt (no bootstrap credential row to reset, or a persistence
// error) is audited too, correlated to the replacement envelope's key_id
// that was sealed before the persistence call failed.
func TestAuditBootstrapCredentialResetFailureRecordsDeniedEvent(t *testing.T) {
	appender := &fakeCLIAuditAppender{}
	auditBootstrapCredentialReset(context.Background(), appender, "key-b", fmt.Errorf("no bootstrap credential row"))

	if !auditEventsContainReason(appender.events, bootstrapCredentialAuditReasonResetFailed) {
		t.Fatalf("no durable audit event for failed credential reset: %#v", appender.events)
	}
	got := appender.events[0]
	if got.Decision != governanceaudit.DecisionDenied {
		t.Fatalf("Decision = %q, want %q", got.Decision, governanceaudit.DecisionDenied)
	}
}

// TestAuditBootstrapCredentialEventsNoPlaintextLeak is the CLI-side mirror of
// go/cmd/api/seed_initial_admin_test.go's TestSeedInitialAdminNegativeLeakage:
// it proves the retrieved/reset plaintext password and recovery code never
// reach a durable audit event. auditBootstrapCredentialRetrieved/Reset take
// only a key_id (never a payload), so this also guards against a future
// change accidentally widening the signature to carry plaintext.
func TestAuditBootstrapCredentialEventsNoPlaintextLeak(t *testing.T) {
	const plaintextPassword = "correct-horse-battery-staple"
	const plaintextRecoveryCode = "recovery-9f8e7d6c5b4a"

	appender := &fakeCLIAuditAppender{}
	auditBootstrapCredentialRetrieved(context.Background(), appender, "key-a", nil)
	auditBootstrapCredentialReset(context.Background(), appender, "key-b", nil)
	auditBootstrapCredentialRetrieved(context.Background(), appender, "key-a", fmt.Errorf("decrypt failed"))
	auditBootstrapCredentialReset(context.Background(), appender, "key-b", fmt.Errorf("reset failed"))

	if auditEventsLeakCLI(appender.events, plaintextPassword, plaintextRecoveryCode) {
		t.Fatalf("audit events leaked plaintext credential material: %#v", appender.events)
	}
}

// TestAuditBootstrapCredentialEventsNilAppenderIsNoop proves a nil appender
// (defensive; every real call site always has an open DB handle) never
// panics, matching every other governance-audit call site in this codebase.
func TestAuditBootstrapCredentialEventsNilAppenderIsNoop(t *testing.T) {
	auditBootstrapCredentialRetrieved(context.Background(), nil, "key-a", nil)
	auditBootstrapCredentialReset(context.Background(), nil, "key-b", nil)
	auditBootstrapCredentialRetrieved(context.Background(), nil, "key-a", fmt.Errorf("decrypt failed"))
	auditBootstrapCredentialReset(context.Background(), nil, "key-b", fmt.Errorf("reset failed"))
}

// TestAuditBootstrapCredentialEventsPersistToRealGovernanceAuditStore is a
// real-Postgres proof that the retrieval/reset audit events actually
// persist. fakeCLIAuditAppender (used by every other test in this file)
// only captures whatever Append receives directly — it never runs
// governanceaudit.NormalizeEvent, so it cannot catch an event that the real
// pgstorage.GovernanceAuditStore would silently reject and drop (every
// Append call site in this codebase discards the error:
// `_ = appender.Append(...)`, matching query.LocalIdentityHandler's
// established fire-and-forget convention). NormalizeEvent requires a
// non-zero OccurredAt; skipped unless a DSN is provided, matching this
// package's other real-Postgres proofs.
func TestAuditBootstrapCredentialEventsPersistToRealGovernanceAuditStore(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the real-Postgres governance-audit persistence proof")
	}
	ctx := context.Background()
	schemaName := fmt.Sprintf("admin_cred_audit_%d", time.Now().UnixNano())
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE") })
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if err := pgstorage.ApplyBootstrap(ctx, pgstorage.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}

	store := pgstorage.NewGovernanceAuditStore(pgstorage.SQLDB{DB: db})
	auditBootstrapCredentialRetrieved(ctx, store, "key-real-a", nil)
	auditBootstrapCredentialReset(ctx, store, "key-real-b", nil)
	auditBootstrapCredentialRetrieved(ctx, store, "key-real-a", fmt.Errorf("decrypt failed"))
	auditBootstrapCredentialReset(ctx, store, "key-real-b", fmt.Errorf("reset failed"))

	retrieved, err := store.List(ctx, pgstorage.GovernanceAuditQuery{
		OperatorAuthorized: true,
		ReasonCode:         bootstrapCredentialAuditReasonRetrieved,
	})
	if err != nil {
		t.Fatalf("List() retrieved events error = %v", err)
	}
	if len(retrieved) != 1 {
		t.Fatalf("persisted retrieval events = %d, want 1 (an event that fails governanceaudit.NormalizeEvent — e.g. a zero OccurredAt — is silently dropped, never returning an error the fire-and-forget caller would see)", len(retrieved))
	}
	if retrieved[0].CorrelationID != "key:key-real-a" {
		t.Fatalf("persisted retrieval event CorrelationID = %q, want %q", retrieved[0].CorrelationID, "key:key-real-a")
	}
	if retrieved[0].OccurredAt.IsZero() {
		t.Fatal("persisted retrieval event OccurredAt is zero")
	}

	retrieveFailed, err := store.List(ctx, pgstorage.GovernanceAuditQuery{
		OperatorAuthorized: true,
		ReasonCode:         bootstrapCredentialAuditReasonRetrieveFailed,
	})
	if err != nil {
		t.Fatalf("List() retrieve-failed events error = %v", err)
	}
	if len(retrieveFailed) != 1 {
		t.Fatalf("persisted retrieve-failed events = %d, want 1", len(retrieveFailed))
	}
	if retrieveFailed[0].Decision != governanceaudit.DecisionDenied {
		t.Fatalf("persisted retrieve-failed event Decision = %q, want %q", retrieveFailed[0].Decision, governanceaudit.DecisionDenied)
	}

	reset, err := store.List(ctx, pgstorage.GovernanceAuditQuery{
		OperatorAuthorized: true,
		ReasonCode:         bootstrapCredentialAuditReasonReset,
	})
	if err != nil {
		t.Fatalf("List() reset events error = %v", err)
	}
	if len(reset) != 1 {
		t.Fatalf("persisted reset events = %d, want 1", len(reset))
	}
	if reset[0].CorrelationID != "key:key-real-b" {
		t.Fatalf("persisted reset event CorrelationID = %q, want %q", reset[0].CorrelationID, "key:key-real-b")
	}

	resetFailed, err := store.List(ctx, pgstorage.GovernanceAuditQuery{
		OperatorAuthorized: true,
		ReasonCode:         bootstrapCredentialAuditReasonResetFailed,
	})
	if err != nil {
		t.Fatalf("List() reset-failed events error = %v", err)
	}
	if len(resetFailed) != 1 {
		t.Fatalf("persisted reset-failed events = %d, want 1", len(resetFailed))
	}
	if resetFailed[0].Decision != governanceaudit.DecisionDenied {
		t.Fatalf("persisted reset-failed event Decision = %q, want %q", resetFailed[0].Decision, governanceaudit.DecisionDenied)
	}
}
