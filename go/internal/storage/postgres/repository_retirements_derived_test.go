// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	lockstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/lock"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The derived-scope fixtures spell the wire form on purpose: they are the value
// the planner writes, so a drifted production constant fails here.
const (
	derivedTestRepoID       = "repository:r_0000000a"
	derivedTestScopeID      = "go-reachability://repository:r_0000000a"
	derivedTestFairnessKey  = "scanner_worker:inst-1:go_reachability"
	derivedTestGenerationID = "derived-gen-1"
)

func derivedTestScope() scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID:       derivedTestScopeID,
		SourceSystem:  "scanner_worker",
		ScopeKind:     scope.KindScannerWorker,
		CollectorKind: scope.CollectorScannerWorker,
		PartitionKey:  derivedTestFairnessKey,
	}
}

func derivedTestGeneration() scope.ScopeGeneration {
	now := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)
	return scope.ScopeGeneration{
		GenerationID: derivedTestGenerationID,
		ScopeID:      derivedTestScopeID,
		ObservedAt:   now,
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
}

// derivedTestStream yields one fact and closes, so a refused commit must drain
// a real stream rather than a nil one.
func derivedTestStream() chan facts.Envelope {
	stream := make(chan facts.Envelope, 1)
	stream <- facts.Envelope{
		FactID: "derived-fact", ScopeID: derivedTestScopeID, GenerationID: derivedTestGenerationID,
		FactKind: "vulnerability.go_module_evidence", StableFactKey: "derived-fact",
		ObservedAt: time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC),
		Payload:    map[string]any{"repository_id": derivedTestRepoID},
	}
	close(stream)
	return stream
}

// sharedBarrierKeys returns the shared advisory keys a commit took, in order.
func sharedBarrierKeys(tx *fakeTx) []string {
	var keys []string
	for _, call := range tx.execs {
		if call.query == lockstore.DeferredMaintenancePartitionedSharedLockSQL {
			keys = append(keys, call.args[1].(string))
		}
	}
	return keys
}

func txIssuedUpsert(tx *fakeTx) bool {
	for _, call := range tx.queries {
		if call.query == upsertIngestionScopeQuery {
			return true
		}
	}
	return false
}

// TestDerivedCommitRefusedWhileParentRetiresOrRetired is the #10054 commit gate:
// a claimed scan that lands after its repository was marked must not recreate
// the derived scope. Its partition key is the planner's fairness key, so the
// folded scope gate cannot see the tombstone: the parent must come from the
// scope id. A complete tombstone refuses too, because only the repository's own
// commit may readmit it.
func TestDerivedCommitRefusedWhileParentRetiresOrRetired(t *testing.T) {
	t.Parallel()

	for _, state := range []string{RetirementStatePending, RetirementStateBlocked, RetirementStateComplete} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()

			tx := &fakeTx{queryResponses: []queueFakeRows{{rows: [][]any{{state}}}}}
			database := &fakeTransactionalDB{tx: tx}
			stream := derivedTestStream()
			err := NewIngestionStore(database).CommitScopeGeneration(
				context.Background(), derivedTestScope(), derivedTestGeneration(), stream)
			if err != nil {
				t.Fatalf("CommitScopeGeneration = %v, want the nil refusal", err)
			}
			if !tx.rolledBack || tx.committed {
				t.Fatalf("rolledBack = %v committed = %v, want a rollback and no commit", tx.rolledBack, tx.committed)
			}
			if txIssuedUpsert(tx) {
				t.Fatal("a refused derived commit issued the scope upsert")
			}
			if len(tx.queries) == 0 || tx.queries[0].args[0] != derivedTestRepoID {
				t.Fatalf("first read = %+v, want the tombstone read keyed by the parent repository", tx.queries)
			}
			for _, call := range tx.execs {
				if strings.Contains(call.query, "readmitted_at") {
					t.Fatalf("derived commit stamped a readmission: %s", call.query)
				}
			}
			if _, open := <-stream; open {
				t.Fatal("the refused commit left the fact stream undrained")
			}
		})
	}
}

// TestDerivedCommitAdmittedWithNoOpenTombstone proves a scan of a live repository
// commits, and that the open-tombstone read precedes the scope upsert.
func TestDerivedCommitAdmittedWithNoOpenTombstone(t *testing.T) {
	t.Parallel()

	tx := &fakeTx{queryResponses: []queueFakeRows{{rows: [][]any{}}}}
	database := &fakeTransactionalDB{tx: tx}
	err := NewIngestionStore(database).CommitScopeGeneration(
		context.Background(), derivedTestScope(), derivedTestGeneration(), nil)
	if err != nil {
		t.Fatalf("CommitScopeGeneration: %v", err)
	}
	if !tx.committed {
		t.Fatal("an admitted derived commit did not commit")
	}
	if !txIssuedUpsert(tx) {
		t.Fatal("an admitted derived commit skipped the scope upsert")
	}
	if len(tx.queries) == 0 || tx.queries[0].query == upsertIngestionScopeQuery {
		t.Fatal("the scope upsert ran before the open-tombstone read")
	}
}

// TestDerivedCommitTakesParentBarrierInSortedOrder proves the fence, which holds
// the parent's exclusive key, waits for an in-flight first scan whose scope row
// it cannot see yet. Keys sort so a maintenance leader taking exclusive keys in
// sorted order cannot deadlock with this commit.
func TestDerivedCommitTakesParentBarrierInSortedOrder(t *testing.T) {
	t.Parallel()

	tx := &fakeTx{queryResponses: []queueFakeRows{{rows: [][]any{}}}}
	database := &fakeTransactionalDB{tx: tx}
	if err := NewIngestionStore(database).CommitScopeGeneration(
		context.Background(), derivedTestScope(), derivedTestGeneration(), nil); err != nil {
		t.Fatalf("CommitScopeGeneration: %v", err)
	}
	got := sharedBarrierKeys(tx)
	if len(got) != 2 || got[0] != derivedTestRepoID || got[1] != derivedTestFairnessKey {
		t.Fatalf("shared barrier keys = %v, want [%s %s]", got, derivedTestRepoID, derivedTestFairnessKey)
	}
}

// TestRepositoryCommitKeepsOneBarrierAndNoExtraRead pins that a scope outside the
// derived family sends exactly the statements it sent before: no second key and
// no tombstone read ahead of the folded gate.
func TestRepositoryCommitKeepsOneBarrierAndNoExtraRead(t *testing.T) {
	t.Parallel()

	for _, scopeID := range []string{
		"git-repository-scope:repository:r_0000000a",
		"xgo-reachability://repository:r_0000000a",
		"go-reachability:/repository:r_0000000a",
		"go-reachability://",
	} {
		t.Run(scopeID, func(t *testing.T) {
			t.Parallel()

			tx := &fakeTx{}
			database := &fakeTransactionalDB{tx: tx}
			scopeValue := testRetiringScopeValue()
			scopeValue.ScopeID = scopeID
			generation := testRetiringScopeGeneration()
			generation.ScopeID = scopeID
			if err := NewIngestionStore(database).CommitScopeGeneration(
				context.Background(), scopeValue, generation, nil); err != nil {
				t.Fatalf("CommitScopeGeneration: %v", err)
			}
			if got := sharedBarrierKeys(tx); len(got) != 1 || got[0] != scopeValue.PartitionKey {
				t.Fatalf("shared barrier keys = %v, want only the partition key", got)
			}
			if len(tx.queries) == 0 || tx.queries[0].query != upsertIngestionScopeQuery {
				t.Fatalf("first statement is not the folded upsert: %+v", tx.queries)
			}
		})
	}
}

// stagePurgeWithDerivedScope stages one fenced purge whose scope lock returns the
// repository's own scope and its derived scope.
func stagePurgeWithDerivedScope(derivedFacts int64) (*fakeTransactionalDB, *fakeTx) {
	tx := &fakeTx{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"rr_derived", derivedTestRepoID, "git-scope", "pending", "fenced"}}},
			{rows: [][]any{{"git-scope"}, {derivedTestScopeID}}},
			{rows: [][]any{}},
			{rows: [][]any{{int64(0), int64(0), int64(0), int64(0)}}},
			{rows: [][]any{{derivedFacts}}},
			{rows: [][]any{{"git-scope", "gen-1"}, {derivedTestScopeID, derivedTestGenerationID}}},
		},
	}
	return &fakeTransactionalDB{tx: tx}, tx
}

// purgeLedger decodes the deleted-row ledger the purge wrote on the tombstone.
func purgeLedger(t *testing.T, tx *fakeTx) map[string]int64 {
	t.Helper()
	for _, call := range tx.execs {
		if call.query == purgeCompleteTombstoneQuery {
			ledger := map[string]int64{}
			if err := json.Unmarshal([]byte(call.args[1].(string)), &ledger); err != nil {
				t.Fatalf("decode ledger: %v", err)
			}
			return ledger
		}
	}
	t.Fatal("the purge wrote no tombstone completion")
	return nil
}

// TestPurgeLedgerNamesDerivedScopeAndFacts proves the ledger accounts for the
// derived scope, so the dry-run preview and the ledger compare row for row. The
// fact count is a pre-count: the scope delete cascades facts and reports nothing.
func TestPurgeLedgerNamesDerivedScopeAndFacts(t *testing.T) {
	t.Parallel()

	database, tx := stagePurgeWithDerivedScope(7)
	result, err := NewRetirementPurger(database, database).PurgeRetired(
		context.Background(), PurgeRequest{RepoIDs: []string{derivedTestRepoID}})
	if err != nil {
		t.Fatalf("PurgeRetired: %v", err)
	}
	if len(result.Completed) != 1 {
		t.Fatalf("result = %+v, want one completion", result)
	}
	ledger := purgeLedger(t, tx)
	if ledger["derived_ingestion_scopes"] != 1 || ledger["derived_fact_records"] != 7 {
		t.Fatalf("ledger = %v, want derived_ingestion_scopes 1 and derived_fact_records 7", ledger)
	}
}

// TestPurgeStatementSelectsDerivedScopeByExactID proves the lock names the
// derived scope by equality on the id built from the repository, never through
// the shared fairness key and never through a prefix match: repository:r_1 must
// not reach go-reachability://repository:r_10.
func TestPurgeStatementSelectsDerivedScopeByExactID(t *testing.T) {
	t.Parallel()

	for name, statement := range map[string]string{
		"purge lock":  purgeLockScopesQuery,
		"capture":     retirementCaptureScopesSQL,
		"fence lock":  fenceLockScopesQuery,
		"fence scope": fenceResolveScopesQuery,
		"fence null":  fenceNullActivePointerQuery,
	} {
		if !strings.Contains(statement, "go-reachability://") {
			t.Errorf("%s does not select the derived scope:\n%s", name, statement)
		}
		upper := strings.ToUpper(statement)
		if strings.Contains(upper, " LIKE ") || strings.Contains(upper, "STARTS_WITH") {
			t.Errorf("%s selects by prefix, which reaches a longer repository id:\n%s", name, statement)
		}
	}
}

// TestDerivedParentRepoIDIsExact pins the one mapping from a scope id to its
// repository, including the look-alikes that must not reach a tombstone.
func TestDerivedParentRepoIDIsExact(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		scopeID string
		want    string
		ok      bool
	}{
		{derivedTestScopeID, derivedTestRepoID, true},
		{"go-reachability://repository:r_0000000a0", "repository:r_0000000a0", true},
		{"go-reachability://", "", false},
		{"go-reachability:/repository:r_0000000a", "", false},
		{"xgo-reachability://repository:r_0000000a", "", false},
		{"GO-REACHABILITY://repository:r_0000000a", "", false},
		{"git-repository-scope:repository:r_0000000a", "", false},
		{derivedTestFairnessKey, "", false},
		{"", "", false},
	} {
		got, ok := derivedParentRepoID(tt.scopeID)
		if got != tt.want || ok != tt.ok {
			t.Errorf("derivedParentRepoID(%q) = %q, %v, want %q, %v", tt.scopeID, got, ok, tt.want, tt.ok)
		}
	}
}

// TestDerivedScopesOfTakesOnlyTheExactScope proves a longer repository id in the
// locked set is not counted as this repository's derived scope.
func TestDerivedScopesOfTakesOnlyTheExactScope(t *testing.T) {
	t.Parallel()

	got := derivedScopesOf(derivedTestRepoID, []string{"git-scope", derivedTestScopeID, derivedTestScopeID + "0"})
	if len(got) != 1 || got[0] != derivedTestScopeID {
		t.Fatalf("derivedScopesOf = %v, want only %s", got, derivedTestScopeID)
	}
	if got := derivedScopesOf(derivedTestRepoID, []string{"git-scope"}); len(got) != 0 {
		t.Fatalf("derivedScopesOf = %v, want none", got)
	}
}

// TestDerivedPrefixMatchesScannerComponent pins the storage literal to the
// constant the scope ids are built from, once that constant lands (#10027). Until
// then the file is absent and the fixtures above pin the wire form. Replace this
// source read with a direct call to scannercomponent.GoReachabilityScopeID when
// #10027 is on the base.
func TestDerivedPrefixMatchesScannerComponent(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "collector", "scannercomponent", "go_reachability.go"))
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("scannercomponent has no Go reachability scope yet (#10027)")
	}
	if err != nil {
		t.Fatalf("read scannercomponent source: %v", err)
	}
	want := fmt.Sprintf("GoReachabilityScopePrefix = %q", goReachabilityScopePrefix)
	if !strings.Contains(string(source), want) {
		t.Fatalf("scannercomponent no longer declares %s: the purge would miss every derived scope", want)
	}
}

// TestPurgeWithoutDerivedScopeSendsNoExtraStatement pins that a repository with no
// derived scope purges with the statements and ledger it always had.
func TestPurgeWithoutDerivedScopeSendsNoExtraStatement(t *testing.T) {
	t.Parallel()

	database, tx := stagePurgeScriptedDB()
	if _, err := NewRetirementPurger(database, database).PurgeRetired(context.Background(), purgeTestRequest()); err != nil {
		t.Fatalf("PurgeRetired: %v", err)
	}
	for _, call := range tx.queries {
		if call.query == purgeDerivedFactCountQuery {
			t.Fatal("a purge with no derived scope ran the derived fact count")
		}
	}
	ledger := purgeLedger(t, tx)
	if _, named := ledger["derived_ingestion_scopes"]; named {
		t.Fatalf("ledger = %v, want no derived entry", ledger)
	}
}

// derivedTestCounter sums one counter from the reader, by outcome attribute.
func derivedTestCounter(t *testing.T, reader *sdkmetric.ManualReader, name string) map[string]int64 {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	totals := map[string]int64{}
	for _, scoped := range collected.ScopeMetrics {
		for _, m := range scoped.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s is %T, want an int64 sum", name, m.Data)
			}
			for _, point := range sum.DataPoints {
				outcome, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionOutcome))
				totals[outcome.AsString()] += point.Value
			}
		}
	}
	return totals
}

func derivedTestInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("derived-test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	return instruments, reader
}

// TestDerivedCommitRefusalCountsAndNamesTheParent proves the two signals of a
// refusal, which otherwise reports success to the claim: the counter by closed
// outcome, and a WARN naming the repository rather than the fairness key.
func TestDerivedCommitRefusalCountsAndNamesTheParent(t *testing.T) {
	t.Parallel()

	for state, wantOutcome := range map[string]string{
		RetirementStatePending:  derivedCommitOutcomeRetiring,
		RetirementStateComplete: derivedCommitOutcomeRetired,
	} {
		instruments, reader := derivedTestInstruments(t)
		var logged bytes.Buffer
		tx := &fakeTx{queryResponses: []queueFakeRows{{rows: [][]any{{state}}}}}
		store := NewIngestionStore(&fakeTransactionalDB{tx: tx})
		store.Instruments = instruments
		store.Logger = slog.New(slog.NewJSONHandler(&logged, nil))
		if err := store.CommitScopeGeneration(context.Background(), derivedTestScope(), derivedTestGeneration(), nil); err != nil {
			t.Fatalf("CommitScopeGeneration(%s): %v", state, err)
		}
		totals := derivedTestCounter(t, reader, "eshu_dp_repository_retirement_derived_commits_refused_total")
		if totals[wantOutcome] != 1 || len(totals) != 1 {
			t.Fatalf("refused counter for %s = %v, want %s:1", state, totals, wantOutcome)
		}
		if !strings.Contains(logged.String(), `"repo_id":"`+derivedTestRepoID+`"`) {
			t.Fatalf("refusal log does not name the parent repository:\n%s", logged.String())
		}
		if !strings.Contains(logged.String(), `"outcome":"repository_`+wantOutcome+`"`) {
			t.Fatalf("refusal WARN outcome for %s = %s, want repository_%s:\n%s", state, logged.String(), wantOutcome, logged.String())
		}
		if !strings.Contains(logged.String(), `"msg":"repository ingest refused: `+wantOutcome+`"`) {
			t.Fatalf("refusal WARN message for %s does not name %s:\n%s", state, wantOutcome, logged.String())
		}
	}
}

// TestPurgeCountsDerivedScopesOnlyWhenPresent proves the purge counter moves by
// the derived scopes it committed, and stays silent for a plain repository.
func TestPurgeCountsDerivedScopesOnlyWhenPresent(t *testing.T) {
	t.Parallel()

	instruments, reader := derivedTestInstruments(t)
	database, _ := stagePurgeWithDerivedScope(3)
	purger := NewRetirementPurger(database, database)
	purger.Instruments = instruments
	if _, err := purger.PurgeRetired(context.Background(), PurgeRequest{RepoIDs: []string{derivedTestRepoID}}); err != nil {
		t.Fatalf("PurgeRetired: %v", err)
	}
	const name = "eshu_dp_repository_retirement_derived_scopes_purged_total"
	if totals := derivedTestCounter(t, reader, name); totals[""] != 1 {
		t.Fatalf("derived scopes purged = %v, want 1", totals)
	}

	plainInstruments, plainReader := derivedTestInstruments(t)
	plainDB, _ := stagePurgeScriptedDB()
	plain := NewRetirementPurger(plainDB, plainDB)
	plain.Instruments = plainInstruments
	if _, err := plain.PurgeRetired(context.Background(), purgeTestRequest()); err != nil {
		t.Fatalf("plain PurgeRetired: %v", err)
	}
	if totals := derivedTestCounter(t, plainReader, name); len(totals) != 0 {
		t.Fatalf("plain purge moved the derived counter: %v", totals)
	}
}

// TestRetirementPlanSeedKeepsDecoyRows pins the plan corpus shape the heavy
// run proved load-bearing: without generations and facts for other scopes,
// scope_generations holds 50 rows and fact_records holds only the target's
// 5,000, so a sequential scan is the planner's correct answer and the plan
// proof fails on its own corpus instead of on a statement.
func TestRetirementPlanSeedKeepsDecoyRows(t *testing.T) {
	t.Parallel()

	seed := strings.Join(retirementPlanSeedStatements("repository:r_00001000"), "\n")
	for _, want := range []string{
		"plan-decoy-gen-", "plan-decoy-fact-", "VACUUM (ANALYZE) fact_records",
	} {
		if !strings.Contains(seed, want) {
			t.Fatalf("plan seed missing decoy marker %q", want)
		}
	}
}
