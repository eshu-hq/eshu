// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite/testutil"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// goReachabilityRepositoryFact is a package consumption fact that names one repository,
// which is how the pass learns the repository ids it asks the Go loader about.
func goReachabilityRepositoryFact(repositoryID string) facts.Envelope {
	return packageConsumptionFactWithRange("fact:11327:"+repositoryID, goCrossScopeTestPackage, repositoryID, "v1.0.0")
}

// goEvidenceWrite is a write that loaded Go evidence from the given
// (go scope, generation) pairs.
func goEvidenceWrite(pairs ...GoEvidenceGeneration) SupplyChainImpactWrite {
	write := retractTestWrite()
	write.GoEvidence = pairs
	return write
}

// TestGoEvidenceLockKeyDiffersFromTheConflictDomainKey pins the key derivation of
// its own (#11327): the shared go-evidence lock must never alias the conflict-domain
// lock of a (scope, generation) finding set, whatever the input, or a pass would
// queue behind its own domain's writers.
func TestGoEvidenceLockKeyDiffersFromTheConflictDomainKey(t *testing.T) {
	t.Parallel()

	scope, generation := "go-reachability://repository:r_1", "gen-1"
	goKey := SupplyChainImpactGoEvidenceLockKey(scope)
	for _, domainKey := range []string{
		supplyChainImpactConflictDomainKey(scope, generation),
		supplyChainImpactConflictDomainKey(scope, ""),
		supplyChainImpactConflictDomainKey("", scope),
	} {
		if goKey == domainKey {
			t.Fatalf("go-evidence lock key %q equals a conflict-domain key", goKey)
		}
	}
	if SupplyChainImpactGoEvidenceLockKey("a") == SupplyChainImpactGoEvidenceLockKey("b") {
		t.Fatal("distinct go scopes share a lock key")
	}
}

// goEvidenceOwedMarker is a stand-in for the production owed-marker statement (it
// updates the pass's own fact_work_items row): it runs one statement on the pass
// transaction, so the order tests see where the writer places it.
type goEvidenceOwedMarker struct{}

const goEvidenceOwedMarkerQuery = "UPDATE fact_work_items SET payload = owed"

func (goEvidenceOwedMarker) RecordFanOutOwed(ctx context.Context, exec factwrite.Execer) error {
	_, err := exec.ExecContext(ctx, goEvidenceOwedMarkerQuery)
	return err
}

// TestWriteLocksGoEvidenceThenRechecksLastBeforeCommit pins the #11327 pass epilogue:
// every other statement of the write transaction runs first, then the shared locks in
// sorted scope order, then one recheck SELECT, then only the owed marker (which updates
// the pass's own work item and so must follow the lock), then the commit. The existing
// order test covers the pass with no resolved pair, which issues neither statement.
func TestWriteLocksGoEvidenceThenRechecksLastBeforeCommit(t *testing.T) {
	t.Parallel()

	inserts := &testutil.FakeExecer{}
	beginner := newFakeImpactBeginner(inserts)
	write := goEvidenceWrite(
		GoEvidenceGeneration{ScopeID: "go-reachability://repository:r_b", GenerationID: "gen-b"},
		GoEvidenceGeneration{ScopeID: "go-reachability://repository:r_a", GenerationID: "gen-a"},
		GoEvidenceGeneration{ScopeID: "go-reachability://repository:r_a", GenerationID: "gen-a"},
	)
	write.Owed = goEvidenceOwedMarker{}
	beginner.state.goEvidenceCurrent = 2
	if _, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(), write); err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	all := beginner.state.all
	n := len(all)
	if n < 5 {
		t.Fatalf("statements = %d, want the pass then the closing lock, recheck and marker", n)
	}
	wantKeys := []string{
		SupplyChainImpactGoEvidenceLockKey("go-reachability://repository:r_a"),
		SupplyChainImpactGoEvidenceLockKey("go-reachability://repository:r_b"),
	}
	for i, key := range wantKeys {
		got := all[n-4+i]
		if got.Query != SupplyChainImpactGoEvidenceSharedLockQuery || got.Args[0] != key {
			t.Fatalf("statement %d from the end = %q %v, want the shared lock of %q (sorted, de-duplicated)", 4-i, got.Query, got.Args, key)
		}
	}
	recheck := all[n-2]
	if recheck.Query != supplyChainImpactGoEvidenceRecheckQuery {
		t.Fatalf("second to last statement = %q, want the recheck", recheck.Query)
	}
	if !slices.Equal(recheck.Args[0].([]string), []string{"go-reachability://repository:r_a", "go-reachability://repository:r_b"}) ||
		!slices.Equal(recheck.Args[1].([]string), []string{"gen-a", "gen-b"}) {
		t.Fatalf("recheck args = %v, want the two resolved pairs", recheck.Args)
	}
	if all[n-1].Query != goEvidenceOwedMarkerQuery {
		t.Fatalf("last statement = %q, want the owed marker after the recheck", all[n-1].Query)
	}
	if violation := goEvidenceEpilogueViolation(queriesOf(all), goEvidenceOwedMarkerQuery); violation != "" {
		t.Fatalf("clean epilogue reported as a violation: %s", violation)
	}
	for i, call := range all[:n-4] {
		if call.Query == SupplyChainImpactGoEvidenceSharedLockQuery || call.Query == supplyChainImpactGoEvidenceRecheckQuery {
			t.Fatalf("statement %d is a go-evidence statement before the closing run", i)
		}
	}
}

func queriesOf(calls []testutil.ExecCall) []string {
	out := make([]string, len(calls))
	for i, call := range calls {
		out[i] = call.Query
	}
	return out
}

// TestGoEvidenceEpilogueViolationNamesTheSeededShapes is the seeded RED half of the
// statement-order guard (#11327 ruling 3.2 and the amendment): a rewrite of the writer
// that moves the shared lock or the recheck earlier, puts another statement after the
// recheck, drops the recheck, or issues one without the lock, is reported.
func TestGoEvidenceEpilogueViolationNamesTheSeededShapes(t *testing.T) {
	t.Parallel()

	lock, recheck, domain, admit, marker := SupplyChainImpactGoEvidenceSharedLockQuery, supplyChainImpactGoEvidenceRecheckQuery,
		lockSupplyChainImpactConflictDomainQuery, supplyChainImpactAdmissionQuery, goEvidenceOwedMarkerQuery
	for name, tc := range map[string]struct {
		statements []string
		want       bool
	}{
		"closing lock, recheck, marker":     {[]string{domain, admit, lock, recheck, marker}, false},
		"closing locks, recheck, no marker": {[]string{domain, admit, lock, lock, recheck}, false},
		"no go evidence at all":             {[]string{domain, admit, marker}, false},
		"lock first (the old position)":     {[]string{lock, recheck, domain, admit}, true},
		"admission after the recheck":       {[]string{domain, lock, recheck, admit}, true},
		"marker before the lock":            {[]string{domain, marker, lock, recheck}, true},
		"two statements after the recheck":  {[]string{domain, lock, recheck, marker, marker}, true},
		"recheck missing":                   {[]string{domain, lock, admit}, true},
		"recheck without a lock":            {[]string{domain, admit, recheck}, true},
		"lock after the recheck":            {[]string{domain, lock, recheck, lock}, true},
		"locks split by another statement":  {[]string{lock, admit, lock, recheck}, true},
	} {
		got := goEvidenceEpilogueViolation(tc.statements, marker) != ""
		if got != tc.want {
			t.Errorf("%s: violation = %v, want %v", name, got, tc.want)
		}
	}
}

// TestWriteAbortsWithTheTypedClassWhenTheRecheckFindsAChangedGeneration pins the
// retry class (#11327 ruling 2): a recheck that finds fewer current pairs than the
// pass loaded returns the typed non-counting error and rolls back: the recheck is the
// last statement, the owed marker never runs, and nothing commits.
func TestWriteAbortsWithTheTypedClassWhenTheRecheckFindsAChangedGeneration(t *testing.T) {
	t.Parallel()

	inserts := &testutil.FakeExecer{}
	beginner := newFakeImpactBeginner(inserts)
	write := goEvidenceWrite(
		GoEvidenceGeneration{ScopeID: "go-reachability://repository:r_a", GenerationID: "gen-a"},
		GoEvidenceGeneration{ScopeID: "go-reachability://repository:r_b", GenerationID: "gen-b"},
	)
	write.Owed = goEvidenceOwedMarker{}
	beginner.state.goEvidenceCurrent = 1
	_, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(), write)
	var changed *SupplyChainImpactGoEvidenceChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("error = %v, want *SupplyChainImpactGoEvidenceChangedError", err)
	}
	if !changed.Retryable() || changed.FailureClass() != SupplyChainImpactGoEvidenceChangedFailureClass {
		t.Fatalf("retryable = %v class = %q, want a retryable error of class %q", changed.Retryable(), changed.FailureClass(), SupplyChainImpactGoEvidenceChangedFailureClass)
	}
	all := beginner.state.all
	if last := all[len(all)-1].Query; last != supplyChainImpactGoEvidenceRecheckQuery {
		t.Fatalf("last statement = %q, want the failed recheck", last)
	}
	for _, call := range all {
		if call.Query == goEvidenceOwedMarkerQuery {
			t.Fatal("the owed marker ran after a failed recheck")
		}
	}
	if beginner.state.commits != 0 || beginner.state.rollbacks != 1 {
		t.Fatalf("commits = %d rollbacks = %d, want a rollback and no commit", beginner.state.commits, beginner.state.rollbacks)
	}
}

// TestWriteWithNoResolvedGoEvidenceIssuesNeitherStatement pins that a pass that
// resolved no go-reachability scope takes no advisory lock and runs no recheck.
func TestWriteWithNoResolvedGoEvidenceIssuesNeitherStatement(t *testing.T) {
	t.Parallel()

	beginner := newFakeImpactBeginner(&testutil.FakeExecer{})
	if _, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(), retractTestWrite()); err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	for _, call := range beginner.state.all {
		if call.Query == SupplyChainImpactGoEvidenceSharedLockQuery || call.Query == supplyChainImpactGoEvidenceRecheckQuery {
			t.Fatalf("a pass with no resolved Go scope issued %q", call.Query)
		}
	}
}

// pairsLoader is a loader with the pairs seam: it resolves two of the three go scopes
// the filter names and returns evidence for both.
type pairsLoader struct {
	goCrossScopeBackedLoader
	resolved GoReachabilityEvidence
}

func (l *pairsLoader) ListActiveGoReachabilityEvidence(
	_ context.Context,
	filter GoReachabilityFactFilter,
) (GoReachabilityEvidence, error) {
	l.goFilters = append(l.goFilters, filter)
	return l.resolved, nil
}

// TestGoEvidenceLoadReturnsExactlyTheResolvedPairs pins ruling 4.7 at the handler:
// three repositories asked, two resolved, the pass carries two pairs to the writer
// and the writer locks and rechecks those two.
func TestGoEvidenceLoadReturnsExactlyTheResolvedPairs(t *testing.T) {
	t.Parallel()

	resolved := GoReachabilityEvidence{Generations: []GoEvidenceGeneration{
		{ScopeID: "go-reachability://repository:r_2", GenerationID: "gen-2"},
		{ScopeID: "go-reachability://repository:r_1", GenerationID: "gen-1"},
	}}
	loader := &pairsLoader{resolved: resolved}
	handler := SupplyChainImpactHandler{FactLoader: loader}
	envelopes := []facts.Envelope{
		goReachabilityRepositoryFact("r_1"), goReachabilityRepositoryFact("r_2"), goReachabilityRepositoryFact("r_3"),
	}
	loaded, err := handler.loadGoReachabilityEvidence(context.Background(), envelopes)
	if err != nil {
		t.Fatalf("loadGoReachabilityEvidence() error = %v", err)
	}
	if len(loader.goFilters) != 1 || len(loader.goFilters[0].RepositoryIDs) != 3 {
		t.Fatalf("filters = %+v, want one load asking about three repositories", loader.goFilters)
	}
	if !slices.Equal(loaded.Generations, resolved.Generations) {
		t.Fatalf("pairs = %v, want exactly the loader's two resolved pairs", loaded.Generations)
	}
	write := handler.impactWrite(reducercontract.Intent{}, nil, supplyChainImpactLoadedEvidence{goEvidence: loaded.Generations}, 1)
	if !slices.Equal(write.GoEvidence, resolved.Generations) {
		t.Fatalf("write.GoEvidence = %v, want the loader's pairs", write.GoEvidence)
	}
	beginner := newFakeImpactBeginner(&testutil.FakeExecer{})
	beginner.state.goEvidenceCurrent = 2
	write.FencingToken = 1
	write.ScopeID, write.GenerationID = "scope", "gen"
	if _, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(), write); err != nil {
		t.Fatalf("write error = %v", err)
	}
	var locks int
	for _, call := range beginner.state.all {
		if call.Query == SupplyChainImpactGoEvidenceSharedLockQuery {
			locks++
		}
		if call.Query == supplyChainImpactGoEvidenceRecheckQuery && len(call.Args[0].([]string)) != 2 {
			t.Fatalf("recheck covers %v, want the two resolved pairs", call.Args[0])
		}
	}
	if locks != 2 {
		t.Fatalf("shared locks = %d, want 2", locks)
	}
}

// TestGoEvidenceFallbackDerivesPairsFromTheLoadedEnvelopes pins the loader without
// the pairs seam: the pairs are the distinct (scope, generation) of the envelopes.
func TestGoEvidenceFallbackDerivesPairsFromTheLoadedEnvelopes(t *testing.T) {
	t.Parallel()

	loader := &goCrossScopeBackedLoader{goFacts: []facts.Envelope{
		{FactID: "a", ScopeID: "go-reachability://repository:r_1", GenerationID: "gen-1"},
		{FactID: "b", ScopeID: "go-reachability://repository:r_1", GenerationID: "gen-1"},
	}}
	loaded, err := (SupplyChainImpactHandler{FactLoader: loader}).loadGoReachabilityEvidence(
		context.Background(), []facts.Envelope{goReachabilityRepositoryFact("r_1")})
	if err != nil {
		t.Fatalf("loadGoReachabilityEvidence() error = %v", err)
	}
	want := []GoEvidenceGeneration{{ScopeID: "go-reachability://repository:r_1", GenerationID: "gen-1"}}
	if !slices.Equal(loaded.Generations, want) || len(loaded.Facts) != 2 {
		t.Fatalf("pairs = %v facts = %d, want one pair and both envelopes", loaded.Generations, len(loaded.Facts))
	}
}

type failingImpactWriter struct{ err error }

func (w failingImpactWriter) WriteSupplyChainImpactFindings(context.Context, SupplyChainImpactWrite) (SupplyChainImpactWriteResult, error) {
	return SupplyChainImpactWriteResult{}, w.err
}

// TestHandlerCountsAGoEvidenceRecheckAbortByReason pins the abort counter (#11327):
// the typed refusal counts once under its closed reason, and any other writer error
// counts nothing.
func TestHandlerCountsAGoEvidenceRecheckAbortByReason(t *testing.T) {
	t.Parallel()

	inst, reader := narrowTestInstruments(t)
	count := func(err error) {
		handler := SupplyChainImpactHandler{Writer: failingImpactWriter{err: err}, Instruments: inst}
		if _, got := handler.writeFindingsWithSuppressionDiff(context.Background(), reducercontract.Intent{}, SupplyChainImpactWrite{}); got == nil {
			t.Fatal("write error was swallowed")
		}
	}
	count(&SupplyChainImpactGoEvidenceChangedError{Cause: GoEvidenceCauseGenerationChanged})
	count(&SupplyChainImpactGoEvidenceChangedError{Cause: GoEvidenceCauseLockTimeout, Err: errors.New("55P03")})
	count(errors.New("some other writer failure"))

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	got := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_go_evidence_recheck_aborts_total" {
				continue
			}
			for _, point := range m.Data.(metricdata.Sum[int64]).DataPoints {
				reason, _ := point.Attributes.Value("reason")
				got[reason.AsString()] += point.Value
			}
		}
	}
	if got[GoEvidenceCauseGenerationChanged] != 1 || got[GoEvidenceCauseLockTimeout] != 1 || len(got) != 2 {
		t.Fatalf("aborts = %v, want one generation_changed and one lock_timeout", got)
	}
}

// TestWriteFailsLoudWhenTheWriteTransactionIsNotReadCommitted pins the isolation guard
// (#11327, ruling 1.4): the recheck is only safe under READ COMMITTED, because under
// REPEATABLE READ the snapshot is taken before the lock wait and the recheck would
// silently pass. A transaction that reports another level fails with a plain error,
// never the non-counting class, so the misconfiguration is loud. Production opens the
// write transaction through SQLDB.Begin, which calls BeginTx with nil options: the
// server's default_transaction_isolation, read committed unless an operator changed it.
func TestWriteFailsLoudWhenTheWriteTransactionIsNotReadCommitted(t *testing.T) {
	t.Parallel()

	beginner := newFakeImpactBeginner(&testutil.FakeExecer{})
	beginner.state.goEvidenceCurrent = 1
	beginner.state.goEvidenceIsolation = "repeatable read"
	_, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(),
		goEvidenceWrite(GoEvidenceGeneration{ScopeID: "go-reachability://repository:r_a", GenerationID: "gen-a"}))
	if err == nil {
		t.Fatal("a repeatable-read write transaction passed the recheck")
	}
	var changed *SupplyChainImpactGoEvidenceChangedError
	if errors.As(err, &changed) {
		t.Fatalf("error = %v, want a plain counting error, not the non-counting class", err)
	}
	if beginner.state.commits != 0 {
		t.Fatalf("commits = %d, want a rollback", beginner.state.commits)
	}
}
