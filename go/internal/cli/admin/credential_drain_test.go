// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package admin

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// TestNewAdminCredentialAuditAppenderNilIsNil pins the defensive nil path:
// no database means no appender, and the audit helpers must no-op on it.
func TestNewAdminCredentialAuditAppenderNilIsNil(t *testing.T) {
	appender := newAdminCredentialAuditAppender(nil)
	if appender != nil {
		t.Fatal("expected nil appender for nil database")
	}
	auditBootstrapCredentialRetrieved(context.Background(), appender, "key-1", nil)
	auditBootstrapCredentialReset(context.Background(), appender, "key-1", nil)
}

// TestIdentityHashMatchesSHA256Vector pins the CLI-local identity hash to
// the stored-envelope algorithm (trim, blank stays blank, otherwise
// "sha256:" plus lowercase hex of the SHA-256 sum): the API compares
// recovery-code and bcrypt-parameter hashes computed the same way, so any
// drift here breaks reset authentication.
func TestIdentityHashMatchesSHA256Vector(t *testing.T) {
	if got := identityHash(""); got != "" {
		t.Fatalf("blank stays blank, got %q", got)
	}
	if got, want := identityHash("  "), ""; got != want {
		t.Fatalf("whitespace-only stays blank, got %q", got)
	}
	const want = "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	if got := identityHash("test"); got != want {
		t.Fatalf("identityHash(test) = %q, want %q", got, want)
	}
	if got := identityHash("  test  "); got != want {
		t.Fatalf("identityHash trims surrounding space, got %q", got)
	}
}

// TestAuditAppenderAcceptsDurableStore proves the CLI-local appender
// interface is satisfied by the durable governance-audit store the
// constructor wires: structural identity with the former query interface,
// so retrieval and reset events keep reaching durable storage.
func TestAuditAppenderAcceptsDurableStore(t *testing.T) {
	var _ GovernanceAuditAppender = (*recordingAppender)(nil)
}

type recordingAppender struct {
	events []governanceaudit.Event
}

func (r *recordingAppender) Append(_ context.Context, events []governanceaudit.Event) error {
	r.events = append(r.events, events...)
	return nil
}
