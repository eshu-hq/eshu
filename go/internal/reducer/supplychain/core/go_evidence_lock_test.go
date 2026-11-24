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

// TestWriteLocksGoEvidenceThenRechecksBeforeAnythingElse pins the #11327 pass
// prologue: the shared locks in sorted scope order, then one recheck SELECT, then the
// conflict-domain lock and the rest. The existing order test covers the pass with no
// resolved pair, which issues neither statement.
func TestWriteLocksGoEvidenceThenRechecksBeforeAnythingElse(t *testing.T) {
	t.Parallel()

	inserts := &testutil.FakeExecer{}
	beginner := newFakeImpactBeginner(inserts)
	write := goEvidenceWrite(
		GoEvidenceGeneration{ScopeID: "go-reachability://repository:r_b", GenerationID: "gen-b"},
		GoEvidenceGeneration{ScopeID: "go-reachability://repository:r_a", GenerationID: "gen-a"},
		GoEvidenceGeneration{ScopeID: "go-reachability://repository:r_a", GenerationID: "gen-a"},
	)
	beginner.state.goEvidenceCurrent = 2
	if _, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(), write); err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	all := beginner.state.all
	if len(all) < 4 {
		t.Fatalf("statements = %d, want the prologue plus the pass", len(all))
	}
	wantKeys := []string{
		SupplyChainImpactGoEvidenceLockKey("go-reachability://repository:r_a"),
		SupplyChainImpactGoEvidenceLockKey("go-reachability://repository:r_b"),
	}
	for i, key := range wantKeys {
		if all[i].Query != SupplyChainImpactGoEvidenceSharedLockQuery || all[i].Args[0] != key {
			t.Fatalf("statement %d = %q %v, want the shared lock of %q (sorted, de-duplicated)", i, all[i].Query, all[i].Args, key)
		}
	}
	if all[2].Query != supplyChainImpactGoEvidenceRecheckQuery {
		t.Fatalf("statement 2 = %q, want the recheck", all[2].Query)
	}
	if !slices.Equal(all[2].Args[0].([]string), []string{"go-reachability://repository:r_a", "go-reachability://repository:r_b"}) ||
		!slices.Equal(all[2].Args[1].([]string), []string{"gen-a", "gen-b"}) {
		t.Fatalf("recheck args = %v, want the two resolved pairs", all[2].Args)
	}
	if all[3].Query != lockSupplyChainImpactConflictDomainQuery {
		t.Fatalf("statement 3 = %q, want the conflict-domain lock right after the recheck", all[3].Query)
	}
	if violation := goEvidencePrologueViolation(queriesOf(all)); violation != "" {
		t.Fatalf("clean prologue reported as a violation: %s", violation)
	}
}

func queriesOf(calls []testutil.ExecCall) []string {
	out := make([]string, len(calls))
	for i, call := range calls {
		out[i] = call.Query
	}
	return out
}

// TestGoEvidencePrologueViolationNamesTheSeededShapes is the seeded RED half of the
// statement-order guard (#11327 ruling 3.2): a rewrite of the writer prologue that
// moves the shared lock or the recheck behind another lock-taking statement, drops
// the recheck, or issues one without the lock, is reported.
func TestGoEvidencePrologueViolationNamesTheSeededShapes(t *testing.T) {
	t.Parallel()

	lock, recheck, domain, admit := SupplyChainImpactGoEvidenceSharedLockQuery, supplyChainImpactGoEvidenceRecheckQuery,
		lockSupplyChainImpactConflictDomainQuery, supplyChainImpactAdmissionQuery
	for name, tc := range map[string]struct {
		statements []string
		want       bool
	}{
		"lock, recheck, domain lock":          {[]string{lock, recheck, domain, admit}, false},
		"no go evidence at all":               {[]string{domain, admit}, false},
		"two locks then recheck":              {[]string{lock, lock, recheck, domain}, false},
		"domain lock before the shared lock":  {[]string{domain, lock, recheck, admit}, true},
		"admission before the recheck":        {[]string{lock, admit, recheck, domain}, true},
		"recheck missing":                     {[]string{lock, domain, admit}, true},
		"recheck without a lock":              {[]string{recheck, domain, admit}, true},
		"recheck before the last shared lock": {[]string{lock, recheck, lock, domain}, true},
	} {
		got := goEvidencePrologueViolation(tc.statements) != ""
		if got != tc.want {
			t.Errorf("%s: violation = %v, want %v", name, got, tc.want)
		}
	}
}

// TestWriteAbortsWithTheTypedClassWhenTheRecheckFindsAChangedGeneration pins the
// retry class (#11327 ruling 2): a recheck that finds fewer current pairs than the
// pass loaded returns the typed non-counting error, issues no later statement, and
// rolls back.
func TestWriteAbortsWithTheTypedClassWhenTheRecheckFindsAChangedGeneration(t *testing.T) {
	t.Parallel()

	inserts := &testutil.FakeExecer{}
	beginner := newFakeImpactBeginner(inserts)
	write := goEvidenceWrite(
		GoEvidenceGeneration{ScopeID: "go-reachability://repository:r_a", GenerationID: "gen-a"},
		GoEvidenceGeneration{ScopeID: "go-reachability://repository:r_b", GenerationID: "gen-b"},
	)
	beginner.state.goEvidenceCurrent = 1
	_, err := (PostgresSupplyChainImpactWriter{DB: beginner}).WriteSupplyChainImpactFindings(context.Background(), write)
	var changed *SupplyChainImpactGoEvidenceChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("error = %v, want *SupplyChainImpactGoEvidenceChangedError", err)
	}
	if !changed.Retryable() || changed.FailureClass() != SupplyChainImpactGoEvidenceChangedFailureClass {
		t.Fatalf("retryable = %v class = %q, want a retryable error of class %q", changed.Retryable(), changed.FailureClass(), SupplyChainImpactGoEvidenceChangedFailureClass)
	}
	if got := len(beginner.state.all); got != 3 {
		t.Fatalf("statements = %d (%v), want 2 locks and the recheck only", got, queriesOf(beginner.state.all))
	}
	if beginner.state.commits != 0 || beginner.state.rollbacks != 1 || len(inserts.Execs) != 0 {
		t.Fatalf("commits = %d rollbacks = %d inserts = %d, want a rollback with nothing written",
			beginner.state.commits, beginner.state.rollbacks, len(inserts.Execs))
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
