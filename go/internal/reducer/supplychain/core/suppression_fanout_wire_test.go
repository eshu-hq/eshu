// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite/testutil"
)

// wireGen is one candidate generation for the writer wire tests.
func wireGen(id string, hour int, admitted, owed bool) suppressionGeneration {
	return suppressionGeneration{
		GenerationID: id,
		Status:       "active",
		ActivatedAt:  time.Date(2026, 11, 4, hour, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, 11, 4, hour, 0, 0, 0, time.UTC),
		Admitted:     admitted,
		Owed:         owed,
	}
}

func wireWrite() SupplyChainImpactWrite {
	write := retractTestWrite()
	write.ScopeID = "scope:10019:operator"
	write.GenerationID = "gen-b"
	write.ScopeFactKinds = []string{chain.VulnerabilitySuppressionFactKind}
	return write
}

func wireBeginner() (fakeImpactBeginner, *fakeImpactTxState) {
	inserts := &testutil.FakeExecer{}
	beginner := newFakeImpactBeginner(inserts)
	return beginner, beginner.state
}

func recordedQueries(state *fakeImpactTxState) []string {
	out := make([]string, 0, len(state.all))
	for _, call := range state.all {
		out = append(out, call.Query)
	}
	return out
}

func hasSuppressionRead(queries []string) bool {
	for _, q := range queries {
		if q == selectSuppressionCandidatesQuery || q == selectSuppressionDiffPairQuery ||
			q == selectSuppressionWholeGenerationQuery || q == setSuppressionDiffStatementTimeoutQuery {
			return true
		}
	}
	return false
}

// TestSuppressionDiffUnionsChangedIDsAndRecordsOwed pins the wiring: a
// suppression-only change names its CVE, lands in ChangedCVEIDs, and sets
// the owed marker through the same condition as a row change.
func TestSuppressionDiffUnionsChangedIDsAndRecordsOwed(t *testing.T) {
	t.Parallel()

	beginner, state := wireBeginner()
	state.candidates = []suppressionGeneration{wireGen("gen-a", 1, true, false), wireGen("gen-b", 2, false, false)}
	state.diffPairs = map[[2]string][]suppressionDiffRow{
		{"gen-a", "gen-b"}: {{SuppressionID: "sup-1", CurrentCVEID: "CVE-2023-38039"}},
	}
	write := wireWrite()
	var owed stubOwedRecorder
	write.Owed = owed.recorder()
	write.Findings = nil

	result, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	if !reflect.DeepEqual(result.ChangedCVEIDs, []string{"CVE-2023-38039"}) {
		t.Fatalf("ChangedCVEIDs = %v, want the suppression CVE", result.ChangedCVEIDs)
	}
	if !reflect.DeepEqual(result.SuppressionCVEIDs, []string{"CVE-2023-38039"}) {
		t.Fatalf("SuppressionCVEIDs = %v, want the suppression CVE", result.SuppressionCVEIDs)
	}
	if !reflect.DeepEqual(result.suppressionDiff.cveIDs, []string{"CVE-2023-38039"}) {
		t.Fatalf("diff report ids = %v, want the suppression CVE", result.suppressionDiff.cveIDs)
	}
	if result.suppressionDiff.action != suppressionDiffRun || result.suppressionDiff.baseline != suppressionBaselineAdmitted {
		t.Fatalf("diff report = %+v, want ran/admitted", result.suppressionDiff)
	}
	if owed.calls != 1 {
		t.Fatalf("owed recordings = %d, want 1", owed.calls)
	}
	if state.commits != 1 || state.rollbacks != 0 {
		t.Fatalf("commits = %d rollbacks = %d, want 1/0", state.commits, state.rollbacks)
	}
}

// stubOwedRecorder counts RecordFanOutOwed calls.
type stubOwedRecorder struct{ calls int }

func (r *stubOwedRecorder) recorder() FanOutOwedRecorder { return r }

func (r *stubOwedRecorder) RecordFanOutOwed(context.Context, factwrite.Execer) error {
	r.calls++
	return nil
}

// TestSuppressionDiffDuplicateWriteReopensNobody pins criterion 2 at the
// writer: a repeat delivery of an admitted generation with no fan-out owed
// issues no diff statement, names no suppression id, and records no marker.
func TestSuppressionDiffDuplicateWriteReopensNobody(t *testing.T) {
	t.Parallel()

	beginner, state := wireBeginner()
	state.candidates = []suppressionGeneration{wireGen("gen-a", 1, true, false), wireGen("gen-b", 2, true, false)}
	state.diffPairs = map[[2]string][]suppressionDiffRow{
		{"gen-a", "gen-b"}: {{SuppressionID: "sup-1", CurrentCVEID: "CVE-2023-38039"}},
	}
	write := wireWrite()
	var owed stubOwedRecorder
	write.Owed = owed.recorder()
	write.Findings = nil

	result, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	if result.suppressionDiff.action != suppressionDiffSkipDuplicate {
		t.Fatalf("diff action = %q, want skipped_duplicate", result.suppressionDiff.action)
	}
	if len(result.SuppressionCVEIDs) != 0 || len(result.ChangedCVEIDs) != 0 {
		t.Fatalf("ChangedCVEIDs = %v SuppressionCVEIDs = %v, want both empty", result.ChangedCVEIDs, result.SuppressionCVEIDs)
	}
	if owed.calls != 0 {
		t.Fatalf("owed recordings = %d, want none", owed.calls)
	}
	for _, q := range recordedQueries(state) {
		if q == selectSuppressionDiffPairQuery || q == selectSuppressionWholeGenerationQuery ||
			q == setSuppressionDiffStatementTimeoutQuery || q == resetSuppressionDiffStatementTimeoutQuery {
			t.Fatalf("duplicate pass issued a walk statement %q: the diff must not run", q)
		}
	}
}

// TestSuppressionDiffSkipsSignalScopesWithoutAStatement pins wiring
// condition 4: a KEV or EPSS scope's pass never reaches the suppression
// reads.
func TestSuppressionDiffSkipsSignalScopesWithoutAStatement(t *testing.T) {
	t.Parallel()

	for _, kinds := range [][]string{
		{chain.VulnerabilityEPSSScoreFactKind},
		{chain.VulnerabilityKnownExploitedFactKind},
		{chain.VulnerabilityCVEFactKind, chain.VulnerabilityAffectedPackageFactKind},
	} {
		beginner, state := wireBeginner()
		write := wireWrite()
		write.ScopeFactKinds = kinds
		write.Findings = nil
		result, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(), write)
		if err != nil {
			t.Fatalf("kinds %v: WriteSupplyChainImpactFindings() error = %v", kinds, err)
		}
		if result.suppressionDiff.action != suppressionDiffSkipKind {
			t.Fatalf("kinds %v: diff action = %q, want skipped_kind", kinds, result.suppressionDiff.action)
		}
		if hasSuppressionRead(recordedQueries(state)) {
			t.Fatalf("kinds %v: pass issued a suppression read: %v", kinds, recordedQueries(state))
		}
	}
}

// TestSuppressionDiffRecoveryReplaysTheWalk pins the crash-after-commit row
// of the retry matrix: a retry owing fan-out repeats the diff over the
// immutable generations and names the same set.
func TestSuppressionDiffRecoveryReplaysTheWalk(t *testing.T) {
	t.Parallel()

	beginner, state := wireBeginner()
	state.candidates = []suppressionGeneration{wireGen("gen-a", 1, true, false), wireGen("gen-b", 2, true, false)}
	state.diffPairs = map[[2]string][]suppressionDiffRow{
		{"gen-a", "gen-b"}: {{SuppressionID: "sup-1", CurrentCVEID: "CVE-2023-38039"}},
	}
	write := wireWrite()
	write.Recovery = true
	write.Findings = nil

	result, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	if result.suppressionDiff.action != suppressionDiffRun {
		t.Fatalf("diff action = %q, want ran", result.suppressionDiff.action)
	}
	if !reflect.DeepEqual(result.ChangedCVEIDs, []string{"CVE-2023-38039"}) {
		t.Fatalf("ChangedCVEIDs = %v, want the replayed suppression CVE", result.ChangedCVEIDs)
	}
}

// TestSuppressionDiffFailClosedOnPendingGeneration pins the writer's
// invariant check: a pending generation rolls back with no admission row,
// no commit, and the existing non-counting deferral error.
func TestSuppressionDiffFailClosedOnPendingGeneration(t *testing.T) {
	t.Parallel()

	beginner, state := wireBeginner()
	pending := suppressionGeneration{GenerationID: "gen-b", Status: "pending", IngestedAt: time.Date(2026, 11, 4, 2, 0, 0, 0, time.UTC)}
	state.candidates = []suppressionGeneration{wireGen("gen-a", 1, true, false), pending}
	write := wireWrite()
	write.Findings = nil

	_, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(), write)
	var notActive reducercontract.GenerationNotYetActiveError
	if !errors.As(err, &notActive) {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v, want GenerationNotYetActiveError", err)
	}
	if state.commits != 0 || state.rollbacks != 1 {
		t.Fatalf("commits = %d rollbacks = %d, want 0/1", state.commits, state.rollbacks)
	}
	for _, q := range recordedQueries(state) {
		if q == supplyChainImpactAdmissionQuery {
			t.Fatalf("fail-closed pass wrote an admission row")
		}
	}
}

// TestSuppressionDiffNoneBaselineNamesEverySuppression pins the first pass:
// with no older generation the whole current generation counts as new, and
// a suppression that names no CVE feeds the no-CVE counter instead of the
// fan-out set.
func TestSuppressionDiffNoneBaselineNamesEverySuppression(t *testing.T) {
	t.Parallel()

	beginner, state := wireBeginner()
	state.candidates = []suppressionGeneration{wireGen("gen-b", 2, false, false)}
	state.wholeGen = []suppressionDiffRow{
		{SuppressionID: "sup-1", CurrentCVEID: "CVE-2023-38039"},
		{SuppressionID: "sup-2"},
	}
	write := wireWrite()
	write.Findings = nil

	result, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	if result.suppressionDiff.action != suppressionDiffRun || result.suppressionDiff.baseline != suppressionBaselineNone {
		t.Fatalf("diff report = %+v, want ran/none", result.suppressionDiff)
	}
	if !reflect.DeepEqual(result.ChangedCVEIDs, []string{"CVE-2023-38039"}) {
		t.Fatalf("ChangedCVEIDs = %v, want only the CVE-scoped suppression", result.ChangedCVEIDs)
	}
	if result.suppressionDiff.noCVEScope != 1 {
		t.Fatalf("no-CVE count = %d, want 1", result.suppressionDiff.noCVEScope)
	}
	if result.suppressionDiff.statements != 1 {
		t.Fatalf("statements = %d, want 1", result.suppressionDiff.statements)
	}
}

// TestSuppressionDiffWalkSpansAnOwedGeneration pins the #10060 baseline at
// the writer: the newest admitted generation still owes fan-out, so the
// walk spans its range from the older cleared admission.
func TestSuppressionDiffWalkSpansAnOwedGeneration(t *testing.T) {
	t.Parallel()

	beginner, state := wireBeginner()
	state.candidates = []suppressionGeneration{
		wireGen("gen-a", 1, true, false),
		wireGen("gen-b", 2, true, true),
		wireGen("gen-c", 3, false, false),
	}
	state.diffPairs = map[[2]string][]suppressionDiffRow{
		{"gen-a", "gen-b"}: {{SuppressionID: "sup-old", CurrentCVEID: "CVE-2023-1"}},
		{"gen-b", "gen-c"}: {{SuppressionID: "sup-new", CurrentCVEID: "CVE-2023-2"}},
	}
	write := wireWrite()
	write.GenerationID = "gen-c"
	write.Findings = nil

	result, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	if result.suppressionDiff.baseline != suppressionBaselineAdmitted {
		t.Fatalf("baseline = %q, want admitted", result.suppressionDiff.baseline)
	}
	if !reflect.DeepEqual(result.ChangedCVEIDs, []string{"CVE-2023-1", "CVE-2023-2"}) {
		t.Fatalf("ChangedCVEIDs = %v, want both pairs' CVEs", result.ChangedCVEIDs)
	}
	if result.suppressionDiff.statements != 2 {
		t.Fatalf("statements = %d, want 2", result.suppressionDiff.statements)
	}
}

// TestSuppressionDiffReadErrorFailsRetryableFanOut pins the failure row: a
// diff read error fails the whole walk with no partial set as the existing
// retryable fan-out class, so the pass retries as a unit.
func TestSuppressionDiffReadErrorFailsRetryableFanOut(t *testing.T) {
	t.Parallel()

	beginner, state := wireBeginner()
	state.candidates = []suppressionGeneration{wireGen("gen-a", 1, true, false), wireGen("gen-b", 2, false, false)}
	state.queryErr = errors.New("injected diff read failure")
	write := wireWrite()
	write.Findings = nil

	_, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(), write)
	if err == nil {
		t.Fatal("WriteSupplyChainImpactFindings() = nil, want the injected read error")
	}
	classifier, ok := err.(failureClassifier)
	if !ok {
		t.Fatalf("error %T does not classify a failure", err)
	}
	if !classifier.Retryable() || classifier.FailureClass() != SupplyChainImpactFanOutFailureClass {
		t.Fatalf("error classifies retryable=%v class=%q, want true/%q",
			classifier.Retryable(), classifier.FailureClass(), SupplyChainImpactFanOutFailureClass)
	}
	if state.commits != 0 || state.rollbacks != 1 {
		t.Fatalf("commits = %d rollbacks = %d, want 0/1", state.commits, state.rollbacks)
	}
}

// TestSuppressionDiffStatementBoundBracketsTheWalk pins wiring condition 2
// at the statement level: the bound is set before the pair read, reset
// after it, and never leaks onto the admission and row writes that follow.
func TestSuppressionDiffStatementBoundBracketsTheWalk(t *testing.T) {
	t.Parallel()

	beginner, state := wireBeginner()
	state.candidates = []suppressionGeneration{wireGen("gen-a", 1, true, false), wireGen("gen-b", 2, false, false)}
	state.diffPairs = map[[2]string][]suppressionDiffRow{
		{"gen-a", "gen-b"}: {{SuppressionID: "sup-1", CurrentCVEID: "CVE-2023-38039"}},
	}
	write := wireWrite()
	write.SuppressionDiffTimeout = 150 * time.Millisecond
	write.Findings = nil

	if _, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(), write); err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	queries := recordedQueries(state)
	setIdx, pairIdx, resetIdx, admitIdx := -1, -1, -1, -1
	for i, q := range queries {
		switch q {
		case setSuppressionDiffStatementTimeoutQuery:
			setIdx = i
		case selectSuppressionDiffPairQuery:
			pairIdx = i
		case resetSuppressionDiffStatementTimeoutQuery:
			resetIdx = i
		case supplyChainImpactAdmissionQuery:
			admitIdx = i
		}
	}
	if setIdx < 0 || pairIdx < 0 || resetIdx < 0 || admitIdx < 0 {
		t.Fatalf("walk framing queries missing: set=%d pair=%d reset=%d admit=%d in %v", setIdx, pairIdx, resetIdx, admitIdx, queries)
	}
	if setIdx >= pairIdx || pairIdx >= resetIdx || resetIdx >= admitIdx {
		t.Fatalf("walk framing out of order: set=%d pair=%d reset=%d admit=%d", setIdx, pairIdx, resetIdx, admitIdx)
	}
	for _, call := range state.all {
		if call.Query == setSuppressionDiffStatementTimeoutQuery {
			if len(call.Args) != 1 || call.Args[0] != "150ms" {
				t.Fatalf("statement timeout args = %v, want [150ms]", call.Args)
			}
		}
	}
}

// TestSuppressionDiffOneHopStillFansOutSuppressionIDs pins the wiring
// hazard: a stamped re-derivation fans out nothing of its own rewrite, but
// the suppression ids pass through regardless of the stamp.
func TestSuppressionDiffOneHopStillFansOutSuppressionIDs(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{holders: []string{"scope:10019:os-holder"}}
	handler := fanOutHandler(port, &changeWriter{result: SupplyChainImpactWriteResult{
		SuppressionCVEIDs: []string{"CVE-2023-38039"},
	}}, nil)
	intent := reducercontract.Intent{
		IntentID:     "intent-onehop",
		ScopeID:      "scope:10019:operator",
		GenerationID: "gen-b",
		Domain:       reducercontract.DomainSupplyChainImpact,
		AttemptCount: 1,
		Payload: map[string]any{
			PayloadKeyFanOutOrigin: map[string]any{"source_scope_id": "scope:10019:other"},
		},
	}
	if _, err := handler.fanOutHolders(context.Background(), intent, SupplyChainImpactWriteResult{
		SuppressionCVEIDs: []string{"CVE-2023-38039"},
	}, fanOutCauseSuppression); err != nil {
		t.Fatalf("fanOutHolders() error = %v", err)
	}
	if !reflect.DeepEqual(port.reopened, []string{"scope:10019:os-holder"}) {
		t.Fatalf("reopened = %v, want the holder: the suppression ids must pass the one-hop stamp", port.reopened)
	}
	if len(port.findCalls) != 1 || !reflect.DeepEqual(port.findCalls[0], []string{"CVE-2023-38039"}) {
		t.Fatalf("holder read CVEs = %v, want the suppression CVE", port.findCalls)
	}
}

// TestSuppressionDiffRetryAfterCrashFansOutTheRecoverySet pins the
// crash-after-commit row at the queue seam: the retry's own rewrite shows no
// change, the owed marker says an earlier attempt committed, and the fan-out
// repeats the recovery set instead of nothing.
func TestSuppressionDiffRetryAfterCrashFansOutTheRecoverySet(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{holders: []string{"scope:10019:os-holder"}}
	handler := fanOutHandler(port, &changeWriter{}, nil)
	intent := fanOutIntent(2, map[string]any{PayloadKeyFanOutOwed: true})
	if _, err := handler.fanOutHolders(context.Background(), intent, SupplyChainImpactWriteResult{
		RecoveryCVEIDs: []string{"CVE-2023-38039"},
	}, fanOutCauseSuppression); err != nil {
		t.Fatalf("fanOutHolders() error = %v", err)
	}
	if !reflect.DeepEqual(port.reopened, []string{"scope:10019:os-holder"}) {
		t.Fatalf("reopened = %v, want the holder: the retry must repeat the recovery set", port.reopened)
	}
	if len(port.findCalls) != 1 || !reflect.DeepEqual(port.findCalls[0], []string{"CVE-2023-38039"}) {
		t.Fatalf("holder read CVEs = %v, want the recovery CVE", port.findCalls)
	}
}
