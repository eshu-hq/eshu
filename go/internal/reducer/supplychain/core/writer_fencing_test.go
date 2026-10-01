// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite/testutil"
)

type failureClassifier interface {
	Retryable() bool
	FailureClass() string
}

// TestSupplyChainImpactWriterStampsFencingTokenOnRows pins #7142: every
// upserted finding row carries the pass's token, so the upsert's conflict guard
// (existing <= EXCLUDED) can rank a stale pass. A row left at the column
// default 0 would make that guard inert for this domain.
func TestSupplyChainImpactWriterStampsFencingTokenOnRows(t *testing.T) {
	t.Parallel()

	inserts := &testutil.FakeExecer{}
	beginner := newFakeImpactBeginner(inserts)
	write := retractTestWrite()
	write.FencingToken = 4242
	if _, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(), write); err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	if len(inserts.Execs) != 1 {
		t.Fatalf("batched inserts = %d, want 1", len(inserts.Execs))
	}
	// The versioned batch insert binds fencing_token last, at $17.
	tokens, ok := inserts.Execs[0].Args[16].([]int64)
	if !ok || len(tokens) != len(write.Findings) {
		t.Fatalf("fencing token arg = %#v, want one int64 per finding", inserts.Execs[0].Args[16])
	}
	for i, token := range tokens {
		if token != 4242 {
			t.Fatalf("row %d fencing_token = %d, want the pass token 4242", i, token)
		}
	}
}

// TestSupplyChainImpactWriterRetractionCarriesFencingToken pins #7142: the
// retraction predicate and the tombstone stamp both use the pass token, so a
// fresher pass's rows (higher token) are never retracted and a tombstone
// records which pass retired it.
func TestSupplyChainImpactWriterRetractionCarriesFencingToken(t *testing.T) {
	t.Parallel()

	beginner := newFakeImpactBeginner(&testutil.FakeExecer{})
	write := retractTestWrite()
	write.FencingToken = 4242
	if _, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(), write); err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	var retract *testutil.ExecCall
	for i := range beginner.state.all {
		if beginner.state.all[i].Query == retractSupersededSupplyChainImpactFindingsQuery {
			retract = &beginner.state.all[i]
		}
	}
	if retract == nil {
		t.Fatal("no retraction issued")
	}
	if got := retract.Args[4]; got != int64(4242) {
		t.Fatalf("retraction $5 = %v, want the pass token 4242", got)
	}
	if !strings.Contains(retract.Query, "fencing_token = $5") || !strings.Contains(retract.Query, "AND fencing_token <= $5") {
		t.Fatalf("retraction must both stamp and be fenced by $5:\n%s", retract.Query)
	}
}

// TestSupplyChainImpactWriterRejectsSupersededPassBeforeUpsert pins #7142: when
// the admission reports 0 rows (a fresher pass already admitted) the pass
// writes and retracts nothing, rolls back, and returns a retryable
// non-counting error rather than a success with zero writes.
func TestSupplyChainImpactWriterRejectsSupersededPassBeforeUpsert(t *testing.T) {
	t.Parallel()

	inserts := &testutil.FakeExecer{}
	beginner := newFakeImpactBeginner(inserts)
	beginner.state.rowsAffected = map[string]int64{supplyChainImpactAdmissionQuery: 0}
	write := retractTestWrite()
	_, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(), write)
	if err == nil {
		t.Fatal("WriteSupplyChainImpactFindings() = nil, want a superseded error")
	}
	var superseded supplyChainImpactWriteSupersededError
	if !errors.As(err, &superseded) {
		t.Fatalf("error = %v, want supplyChainImpactWriteSupersededError", err)
	}
	var classified failureClassifier
	if !errors.As(err, &classified) || !classified.Retryable() || classified.FailureClass() != SupplyChainImpactWriteSupersededFailureClass {
		t.Fatalf("error is not retryable with class %q", SupplyChainImpactWriteSupersededFailureClass)
	}
	for _, call := range beginner.state.all {
		if call.Query == factwrite.BatchInsertVersionedQuery || call.Query == retractSupersededSupplyChainImpactFindingsQuery {
			t.Fatalf("a rejected pass issued %q; it must write and retract nothing", call.Query)
		}
	}
	if beginner.state.commits != 0 || beginner.state.rollbacks != 1 {
		t.Fatalf("commits=%d rollbacks=%d, want 0/1", beginner.state.commits, beginner.state.rollbacks)
	}
}

// TestSupplyChainImpactWriterRejectsZeroFencingToken pins the fail-closed
// contract: a write that never got a token is rejected before a transaction
// opens, not written with the inert default 0.
func TestSupplyChainImpactWriterRejectsZeroFencingToken(t *testing.T) {
	t.Parallel()

	beginner := newFakeImpactBeginner(&testutil.FakeExecer{})
	write := retractTestWrite()
	write.FencingToken = 0
	_, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(), write)
	if !errors.Is(err, errSupplyChainImpactMissingFencingToken) {
		t.Fatalf("error = %v, want errSupplyChainImpactMissingFencingToken", err)
	}
	if len(beginner.state.all) != 0 {
		t.Fatalf("statements = %d, want none for a token-less write", len(beginner.state.all))
	}
}

// TestSupplyChainImpactWriterPartialEvidenceStillAdmits pins #7142 against
// #7154: a partial-evidence pass is fenced and stamped like any other; only its
// retraction is skipped. A stale partial pass would otherwise upsert stale rows
// into a fresher set.
func TestSupplyChainImpactWriterPartialEvidenceStillAdmits(t *testing.T) {
	t.Parallel()

	inserts := &testutil.FakeExecer{}
	beginner := newFakeImpactBeginner(inserts)
	write := retractTestWrite()
	write.PartialEvidence = true
	if _, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(), write); err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	var admitted, retracted bool
	for _, call := range beginner.state.all {
		admitted = admitted || call.Query == supplyChainImpactAdmissionQuery
		retracted = retracted || call.Query == retractSupersededSupplyChainImpactFindingsQuery
	}
	if !admitted || retracted {
		t.Fatalf("admitted=%v retracted=%v, want admitted without retraction", admitted, retracted)
	}
	if tokens := inserts.Execs[0].Args[16].([]int64); tokens[0] != write.FencingToken {
		t.Fatalf("partial pass row token = %d, want %d", tokens[0], write.FencingToken)
	}
}

// TestSupplyChainImpactWriterAdmissionAdmitsEqualToken pins the `<=`: a
// re-execution of the same write value carries the same token and must be
// admitted. The query text is the contract here; the live test exercises the same replay on Postgres.
func TestSupplyChainImpactWriterAdmissionAdmitsEqualToken(t *testing.T) {
	t.Parallel()

	if !strings.Contains(supplyChainImpactAdmissionQuery, "fencing_token <= EXCLUDED.fencing_token") {
		t.Fatalf("admission must admit an equal token (<=, not <):\n%s", supplyChainImpactAdmissionQuery)
	}
}
