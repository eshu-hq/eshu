// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// This file guards the invariant README.md and AGENTS.md both state: no
// caller can retrieve or reset the bootstrap admin credential without
// recording the attempt. Until it existed, nothing did. Deleting the
// auditBootstrapCredentialRetrieved call from RetrieveInitialCredential (or
// the auditBootstrapCredentialReset call from ResetInitialCredential) left
// `go test ./internal/cli/admin/` fully green while shipping a credential
// path that writes no audit row — credential_audit_test.go calls the audit
// helpers directly and never drives either exported function, and the two
// tests that would have caught it need ESHU_POSTGRES_DSN and skip without it.
//
// The tests below need no Postgres. Both exported functions take a
// pgstorage.ExecQueryer, so fakeCredentialDB stands in for the connection and
// records every statement they drive, including the INSERT the real
// pgstorage.GovernanceAuditStore issues. Asserting on that INSERT rather than
// on a stubbed appender keeps governanceaudit.NormalizeEvent in the path: an
// event it rejects never reaches ExecContext, and Append's error is discarded
// by the fire-and-forget call site, so a rejected event looks exactly like a
// missing one here — which is the same way it looks in production.

// Column positions in insertGovernanceAuditEventsPrefix's 15-column list
// (governance_audit_store.go). Reading the recorded args positionally couples
// this file to that column order on purpose: a reordering that silently
// changed which value lands in reason_code should break a test, and
// wantExactlyOneAuditEvent asserts the column count too.
const (
	governanceAuditColumnCount         = 15
	governanceAuditDecisionColumnIndex = 7
	governanceAuditReasonColumnIndex   = 8
	governanceAuditKeyIDColumnIndex    = 9
)

// TestRetrieveInitialCredentialAuditsSuccessfulRetrieval drives the exported
// function end to end against a fake connection and proves the success path
// writes its durable retrieval event.
func TestRetrieveInitialCredentialAuditsSuccessfulRetrieval(t *testing.T) {
	keyring := newCredentialInvariantKeyring(t, "key-a")
	db := &fakeCredentialDB{
		envelopeFound:  true,
		sealedEnvelope: sealCredentialInvariantEnvelope(t, keyring, `{"username":"admin","password":"pw","recovery_code":"rc"}`),
		envelopeKeyID:  "key-a",
	}

	payload, err := RetrieveInitialCredential(context.Background(), db, keyring)
	if err != nil {
		t.Fatalf("RetrieveInitialCredential() error = %v", err)
	}
	if payload.Username != "admin" || payload.Password != "pw" {
		t.Fatalf("RetrieveInitialCredential() payload = %#v, want the sealed bundle", payload)
	}

	got := wantExactlyOneAuditEvent(t, db, "successful retrieval")
	if got.reason != bootstrapCredentialAuditReasonRetrieved {
		t.Fatalf("audit reason_code = %q, want %q", got.reason, bootstrapCredentialAuditReasonRetrieved)
	}
	if got.decision != string(governanceaudit.DecisionAllowed) {
		t.Fatalf("audit decision = %q, want %q", got.decision, governanceaudit.DecisionAllowed)
	}
	if got.keyID != "key:key-a" {
		t.Fatalf("audit correlation_id = %v, want %q", got.keyID, "key:key-a")
	}
}

// TestRetrieveInitialCredentialAuditsFailedRetrieval proves a retrieval that
// finds no envelope still records the attempt. A failed attempt is as
// security-relevant as a successful one: it tells an operator someone reached
// for a credential that was already consumed or reset.
func TestRetrieveInitialCredentialAuditsFailedRetrieval(t *testing.T) {
	keyring := newCredentialInvariantKeyring(t, "key-a")
	db := &fakeCredentialDB{envelopeFound: false}

	if _, err := RetrieveInitialCredential(context.Background(), db, keyring); err == nil {
		t.Fatal("RetrieveInitialCredential() error = nil, want the no-retrievable-credential error")
	}

	got := wantExactlyOneAuditEvent(t, db, "failed retrieval")
	if got.reason != bootstrapCredentialAuditReasonRetrieveFailed {
		t.Fatalf("audit reason_code = %q, want %q", got.reason, bootstrapCredentialAuditReasonRetrieveFailed)
	}
	if got.decision != string(governanceaudit.DecisionDenied) {
		t.Fatalf("audit decision = %q, want %q", got.decision, governanceaudit.DecisionDenied)
	}
}

// TestResetInitialCredentialAuditsSuccessfulReset drives the exported reset
// through the whole transaction the fake stands in for, and proves the
// success path writes its durable reset event.
func TestResetInitialCredentialAuditsSuccessfulReset(t *testing.T) {
	keyring := newCredentialInvariantKeyring(t, "key-a")
	db := &fakeCredentialDB{
		subjectIDHash: "sha256:bootstrap-subject",
		ownerUserID:   "user-bootstrap",
	}

	payload, err := ResetInitialCredential(context.Background(), db, keyring, "admin")
	if err != nil {
		t.Fatalf("ResetInitialCredential() error = %v", err)
	}
	if payload.Password == "" || payload.RecoveryCode == "" {
		t.Fatalf("ResetInitialCredential() payload = %#v, want a fresh password and recovery code", payload)
	}

	got := wantExactlyOneAuditEvent(t, db, "successful reset")
	if got.reason != bootstrapCredentialAuditReasonReset {
		t.Fatalf("audit reason_code = %q, want %q", got.reason, bootstrapCredentialAuditReasonReset)
	}
	if got.decision != string(governanceaudit.DecisionAllowed) {
		t.Fatalf("audit decision = %q, want %q", got.decision, governanceaudit.DecisionAllowed)
	}
	// The key id belongs to the freshly sealed replacement envelope, so it is
	// unknown to this test up front; only its shape can be asserted.
	if !strings.HasPrefix(got.keyID, "key:") || got.keyID == "key:" {
		t.Fatalf("audit correlation_id = %q, want a key:<id> correlation to the replacement envelope", got.keyID)
	}
}

// TestResetInitialCredentialAuditsFailedReset proves a reset that finds no
// bootstrap credential row to replace still records the attempt. The fake
// returns no subject row, which is what ResetBootstrapCredential turns into
// pgstorage.ErrBootstrapCredentialNotFound.
func TestResetInitialCredentialAuditsFailedReset(t *testing.T) {
	keyring := newCredentialInvariantKeyring(t, "key-a")
	db := &fakeCredentialDB{ownerUserID: "user-bootstrap"}

	_, err := ResetInitialCredential(context.Background(), db, keyring, "admin")
	if err == nil {
		t.Fatal("ResetInitialCredential() error = nil, want the no-bootstrap-credential error")
	}
	if !strings.Contains(err.Error(), "no bootstrap credential exists") {
		t.Fatalf("ResetInitialCredential() error = %q, want the documented not-found guidance", err.Error())
	}

	got := wantExactlyOneAuditEvent(t, db, "failed reset")
	if got.reason != bootstrapCredentialAuditReasonResetFailed {
		t.Fatalf("audit reason_code = %q, want %q", got.reason, bootstrapCredentialAuditReasonResetFailed)
	}
	if got.decision != string(governanceaudit.DecisionDenied) {
		t.Fatalf("audit decision = %q, want %q", got.decision, governanceaudit.DecisionDenied)
	}
}

// appendedAuditEvent is the audit row as it reached the database driver,
// read back out of the recorded INSERT arguments.
type appendedAuditEvent struct {
	reason   string
	decision string
	keyID    string
}

// wantExactlyOneAuditEvent is the single assertion all four tests above share,
// so the success and failure cases exercise the same extraction. operation
// names the credential operation under test, so a missing event says which
// one went unaudited.
func wantExactlyOneAuditEvent(t *testing.T, db *fakeCredentialDB, operation string) appendedAuditEvent {
	t.Helper()
	events := db.appendedAuditEvents(t)
	if len(events) == 0 {
		t.Fatalf(
			"%s wrote no governance-audit row: the credential operation completed unaudited, breaking the invariant that no caller can retrieve or reset without recording the attempt",
			operation,
		)
	}
	if len(events) != 1 {
		t.Fatalf("%s wrote %d governance-audit rows, want exactly 1: %#v", operation, len(events), events)
	}
	return events[0]
}

// recordedStatement is one statement the credential code drove through the
// fake connection.
type recordedStatement struct {
	sql  string
	args []any
}

// fakeCredentialDB is a hermetic pgstorage.ExecQueryer that records every
// statement RetrieveInitialCredential and ResetInitialCredential drive. It
// also implements pgstorage.Beginner, which ResetBootstrapCredential requires
// for its transaction.
//
// The query routing below matches production SQL by substring. That couples
// the fake to statements it does not own, which is a deliberate trade: if one
// of those SELECTs changes shape, the fake returns no rows and the test fails
// loudly rather than passing on a stale assumption.
type fakeCredentialDB struct {
	mu         sync.Mutex
	statements []recordedStatement

	// sealedEnvelope and envelopeKeyID are the row SelectBootstrapCredential
	// reads, returned only when envelopeFound is true.
	sealedEnvelope string
	envelopeKeyID  string
	envelopeFound  bool

	// subjectIDHash and ownerUserID are the two rows ResetBootstrapCredential
	// reads inside its transaction. Leaving subjectIDHash empty drives the
	// not-found failure without a Postgres.
	subjectIDHash string
	ownerUserID   string
}

func (f *fakeCredentialDB) record(query string, args []any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statements = append(f.statements, recordedStatement{sql: query, args: args})
}

func (f *fakeCredentialDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	f.record(query, args)
	return fakeCredentialResult{}, nil
}

func (f *fakeCredentialDB) QueryContext(_ context.Context, query string, args ...any) (pgstorage.Rows, error) {
	f.record(query, args)
	switch {
	case strings.Contains(query, "SELECT sealed_credential"):
		if !f.envelopeFound {
			return &fakeCredentialRows{}, nil
		}
		return &fakeCredentialRows{rows: [][]string{{f.sealedEnvelope, f.envelopeKeyID}}}, nil
	case strings.Contains(query, "SELECT subject_id_hash"):
		if f.subjectIDHash == "" {
			return &fakeCredentialRows{}, nil
		}
		return &fakeCredentialRows{rows: [][]string{{f.subjectIDHash}}}, nil
	case strings.Contains(query, "SELECT user_id"):
		if f.ownerUserID == "" {
			return &fakeCredentialRows{}, nil
		}
		return &fakeCredentialRows{rows: [][]string{{f.ownerUserID}}}, nil
	default:
		return nil, fmt.Errorf("fakeCredentialDB has no row set for query: %s", query)
	}
}

// Begin satisfies pgstorage.Beginner. Statements inside the transaction land
// in the same recording as statements outside it, which is what lets the
// audit assertion see the reset's INSERT.
func (f *fakeCredentialDB) Begin(context.Context) (pgstorage.Transaction, error) {
	return fakeCredentialTx{db: f}, nil
}

// appendedAuditEvents reads back every governance-audit row the recorded
// statements inserted.
func (f *fakeCredentialDB) appendedAuditEvents(t *testing.T) []appendedAuditEvent {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	var events []appendedAuditEvent
	for _, stmt := range f.statements {
		if !strings.Contains(stmt.sql, "INSERT INTO governance_audit_events") {
			continue
		}
		if len(stmt.args) != governanceAuditColumnCount {
			t.Fatalf("governance-audit INSERT carried %d args, want %d: the column list moved", len(stmt.args), governanceAuditColumnCount)
		}
		events = append(events, appendedAuditEvent{
			reason:   auditArgString(t, stmt.args, governanceAuditReasonColumnIndex),
			decision: auditArgString(t, stmt.args, governanceAuditDecisionColumnIndex),
			keyID:    auditArgString(t, stmt.args, governanceAuditKeyIDColumnIndex),
		})
	}
	return events
}

// auditArgString reads one column out of a recorded INSERT. A NULL column
// (nullableGovernanceAuditString returns nil for a blank value) reads as "".
func auditArgString(t *testing.T, args []any, index int) string {
	t.Helper()
	if args[index] == nil {
		return ""
	}
	value, ok := args[index].(string)
	if !ok {
		t.Fatalf("governance-audit INSERT arg %d = %T, want string", index, args[index])
	}
	return value
}

// fakeCredentialTx routes the reset transaction's statements back into the
// connection's recording and commits without doing anything.
type fakeCredentialTx struct {
	db *fakeCredentialDB
}

func (t fakeCredentialTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.db.ExecContext(ctx, query, args...)
}

func (t fakeCredentialTx) QueryContext(ctx context.Context, query string, args ...any) (pgstorage.Rows, error) {
	return t.db.QueryContext(ctx, query, args...)
}

func (t fakeCredentialTx) Commit() error   { return nil }
func (t fakeCredentialTx) Rollback() error { return nil }

// fakeCredentialResult reports one affected row, which is what
// ResetBootstrapCredential's password rotation requires to proceed.
type fakeCredentialResult struct{}

func (fakeCredentialResult) LastInsertId() (int64, error) {
	return 0, errors.New("fakeCredentialResult does not report insert ids")
}

func (fakeCredentialResult) RowsAffected() (int64, error) { return 1, nil }

type fakeCredentialRows struct {
	rows  [][]string
	index int
}

func (r *fakeCredentialRows) Next() bool { return r.index < len(r.rows) }

func (r *fakeCredentialRows) Scan(dest ...any) error {
	row := r.rows[r.index]
	if len(dest) != len(row) {
		return fmt.Errorf("fakeCredentialRows.Scan: %d destinations for a %d-column row", len(dest), len(row))
	}
	for i := range dest {
		target, ok := dest[i].(*string)
		if !ok {
			return fmt.Errorf("fakeCredentialRows.Scan: destination %d is %T, want *string", i, dest[i])
		}
		*target = row[i]
	}
	r.index++
	return nil
}

func (r *fakeCredentialRows) Err() error   { return nil }
func (r *fakeCredentialRows) Close() error { return nil }

func newCredentialInvariantKeyring(t *testing.T, keyID string) *secretcrypto.Keyring {
	t.Helper()
	keyring, err := secretcrypto.NewKeyring(
		secretcrypto.KeyID(keyID),
		map[secretcrypto.KeyID][]byte{secretcrypto.KeyID(keyID): bytes.Repeat([]byte{7}, 32)},
	)
	if err != nil {
		t.Fatalf("NewKeyring() error = %v", err)
	}
	return keyring
}

func sealCredentialInvariantEnvelope(t *testing.T, keyring *secretcrypto.Keyring, plaintext string) string {
	t.Helper()
	aad := pgstorage.BootstrapCredentialAAD(pgstorage.BootstrapAdminTenantID, pgstorage.BootstrapAdminWorkspaceID)
	sealed, err := keyring.Seal([]byte(plaintext), aad)
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	return sealed
}
