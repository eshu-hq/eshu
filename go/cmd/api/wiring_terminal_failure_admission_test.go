// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/query/admin"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// fakeTerminalFailureAdmission is a no-store admission port: the handler
// test below proves the handler carries the injected versioned admission
// contract without opening a direct table handle.
type fakeTerminalFailureAdmission struct{}

func (f *fakeTerminalFailureAdmission) ResolveTarget(
	_ context.Context,
	_ pgstorage.TerminalFailureAckRequest,
) (pgstorage.TerminalFailureAckTarget, error) {
	return pgstorage.TerminalFailureAckTarget{}, nil
}

func (f *fakeTerminalFailureAdmission) Acknowledge(
	_ context.Context,
	_ pgstorage.TerminalFailureAckRequest,
) (pgstorage.TerminalFailureAckResult, error) {
	return pgstorage.TerminalFailureAckResult{}, nil
}

func (f *fakeTerminalFailureAdmission) ListTerminalFailureAcks(
	_ context.Context,
	_ pgstorage.TerminalFailureAckListFilter,
) ([]pgstorage.TerminalFailureAckRecord, error) {
	return nil, nil
}

func (f *fakeTerminalFailureAdmission) ListTerminalFailures(
	_ context.Context,
	_ pgstorage.TerminalFailureListFilter,
) (pgstorage.TerminalFailureListPage, error) {
	return pgstorage.TerminalFailureListPage{}, nil
}

// fakeTerminalFailureLedger is a no-store replay ledger for the wiring
// test: claiming and completing succeed without touching Postgres.
type fakeTerminalFailureLedger struct{}

func (f *fakeTerminalFailureLedger) ClaimReplayIdempotency(
	_ context.Context,
	_, _ string,
	_ time.Time,
) (admin.ReplayIdempotencyClaim, error) {
	return admin.ReplayIdempotencyClaim{}, nil
}

func (f *fakeTerminalFailureLedger) CompleteReplayIdempotency(
	_ context.Context,
	_ string,
	_ int,
	_ []string,
	_ time.Time,
) error {
	return nil
}

// TestNewTerminalFailureHandlerTakesAdmissionContract proves the #8872
// seam: the terminal-failure handler is built with the versioned
// acknowledgement admission contract, not a *sql.DB writer handle. The
// handler must carry the exact injected port (identity, not merely
// non-nil) so eshu-api can later bind a core-issued admission endpoint.
func TestNewTerminalFailureHandlerTakesAdmissionContract(t *testing.T) {
	t.Parallel()

	admission := &fakeTerminalFailureAdmission{}
	handler := newTerminalFailureHandler(admission, &fakeTerminalFailureLedger{}, nil, nil)
	if handler == nil {
		t.Fatal("newTerminalFailureHandler() = nil, want a handler bound to the admission contract")
	}
	if handler.Acknowledger != query.TerminalFailureAdmission(admission) {
		t.Fatalf("handler.Acknowledger = %T(%v), want the exact injected admission contract %T(%v)", handler.Acknowledger, handler.Acknowledger, admission, admission)
	}
}

// TestNewTerminalFailureHandlerTakesLedgerContract proves the #8872
// replay-ledger seam: the handler is built with the versioned ledger
// admission contract, not a direct ledger handle. The handler must carry
// the exact injected port (identity, not merely non-nil) so eshu-api can
// later bind a core-issued ledger endpoint.
func TestNewTerminalFailureHandlerTakesLedgerContract(t *testing.T) {
	t.Parallel()

	ledger := &fakeTerminalFailureLedger{}
	handler := newTerminalFailureHandler(&fakeTerminalFailureAdmission{}, ledger, nil, nil)
	if handler == nil {
		t.Fatal("newTerminalFailureHandler() = nil, want a handler bound to the ledger contract")
	}
	if handler.Ledger != query.ReplayLedgerAdmission(ledger) {
		t.Fatalf("handler.Ledger = %T(%v), want the exact injected ledger contract %T(%v)", handler.Ledger, handler.Ledger, ledger, ledger)
	}
}
