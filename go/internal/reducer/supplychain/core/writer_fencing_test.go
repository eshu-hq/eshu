// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite/testutil"
)

type failureClassifier interface {
	Retryable() bool
	FailureClass() string
}

// TestSupplyChainImpactWriterTokenReachesOnlyTheAdmission pins #7142 after the
// PR-C2 write stop (#10880): the pass's fencing token is decided once, by the
// admission row, which rejects a stale pass whole. The rows handed to the supports
// maintainer carry no token (a support has no token column and no per-row guard),
// and the admission statement receives the pass token.
func TestSupplyChainImpactWriterTokenReachesOnlyTheAdmission(t *testing.T) {
	t.Parallel()

	inserts := &testutil.FakeExecer{}
	beginner := newFakeImpactBeginner(inserts)
	write := retractTestWrite()
	write.FencingToken = 4242
	if _, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(), write); err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	if len(inserts.Execs) != 1 {
		t.Fatalf("supports upsert batches = %d, want 1", len(inserts.Execs))
	}
	// The fake maintainer re-encodes the rows it received in the batched versioned
	// shape, which binds fencing_token last, at $17.
	tokens, ok := inserts.Execs[0].Args[16].([]int64)
	if !ok || len(tokens) != len(write.Findings) {
		t.Fatalf("fencing token arg = %#v, want one int64 per finding", inserts.Execs[0].Args[16])
	}
	for i, token := range tokens {
		if token != 0 {
			t.Fatalf("row %d carries fencing token %d, want none: the admission alone orders passes", i, token)
		}
	}
	var admission *testutil.ExecCall
	for i := range beginner.state.all {
		if beginner.state.all[i].Query == supplyChainImpactAdmissionQuery {
			admission = &beginner.state.all[i]
		}
	}
	if admission == nil {
		t.Fatal("no admission statement issued")
	}
	found := false
	for _, arg := range admission.Args {
		found = found || arg == int64(4242)
	}
	if !found {
		t.Fatalf("admission args = %v, want the pass token 4242 among them", admission.Args)
	}
}

// TestSupplyChainImpactWriterRetractionHasNoPerRowFencingToken pins the #10880
// PR-C2 i change of #7142's guard: the retraction deletes supports without a
// per-row token predicate or stamp, because a stale pass is rejected whole by the
// admission row before any statement runs and the conflict-domain lock serializes
// the passes of one pair (arbiter-prc-design F4). Its arguments are the scope, the
// generation and the keep set, and it names neither a token nor fact_records.
func TestSupplyChainImpactWriterRetractionHasNoPerRowFencingToken(t *testing.T) {
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
	if len(retract.Args) != 3 || retract.Args[0] != write.ScopeID || retract.Args[1] != write.GenerationID {
		t.Fatalf("retraction args = %v, want scope, generation and the keep set", retract.Args)
	}
	for _, forbidden := range []string{"fencing_token", "fact_records", "is_tombstone"} {
		if strings.Contains(retract.Query, forbidden) {
			t.Fatalf("the retraction names %q: it must delete supports only, with no per-row token:\n%s", forbidden, retract.Query)
		}
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
		if call.Query == fakeSupportsUpsertQuery || call.Query == retractSupersededSupplyChainImpactFindingsQuery {
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
	if len(inserts.Execs) != 1 {
		t.Fatalf("supports upsert batches = %d, want the partial pass to upsert its rows", len(inserts.Execs))
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
