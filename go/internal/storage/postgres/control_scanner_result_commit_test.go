// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/ospackagevulnerability"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type scannerResultMembershipDB struct {
	rows             [][]any
	execQueries      []string
	requestedFactIDs []string
}

func (f *scannerResultMembershipDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	if query != scannerResultFactMembershipQuery || len(args) != 3 {
		return nil, errors.New("unexpected scanner result query")
	}
	ids, ok := args[2].([]string)
	if !ok {
		return nil, errors.New("scanner result query did not filter emitted IDs")
	}
	f.requestedFactIDs = append([]string(nil), ids...)
	return &queueFakeRows{rows: f.rows}, nil
}

func (f *scannerResultMembershipDB) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	f.execQueries = append(f.execQueries, query)
	if query == scannerResultReceiptInsertQuery {
		return fakeRowsAffected{n: 1}, nil
	}
	if query == scannerResultFactAssociationsInsertQuery {
		return fakeRowsAffected{n: int64(len(f.rows))}, nil
	}
	return nil, errors.New("unexpected scanner result write")
}

func TestScannerResultMembershipRequiresExactPersistedFacts(t *testing.T) {
	t.Parallel()
	const scopeID = "image://derived/target"
	const generationID = "scan-generation"
	claim := workflow.ClaimMutation{FencingToken: 7, ObservedAt: time.Now().UTC()}
	receipt := scannerAuthorityReceipt{
		ScanScopeID: scopeID, ScanGenerationID: generationID,
		WorkItemID: "work", BindingID: "binding", ClaimID: "claim",
		ClaimFencingToken: 7, ClaimOwnerID: "owner",
		CompletedAuthorityVector: []byte(`{"source_epoch":1}`),
	}
	valid := []any{"fact-analysis", "scanner_worker.analysis", int64(7), "scanner_worker", false}
	for _, test := range []struct {
		name      string
		rows      [][]any
		expected  map[string]string
		wantError bool
	}{
		{name: "exact", rows: [][]any{valid}, expected: map[string]string{"fact-analysis": "scanner_worker.analysis"}},
		{name: "package warning", rows: [][]any{{
			"fact-warning", chain.VulnerabilityWarningFactKind, int64(7),
			"scanner_worker", false,
		}}, expected: map[string]string{"fact-warning": chain.VulnerabilityWarningFactKind}},
		{name: "missing", rows: nil, expected: map[string]string{"fact-analysis": "scanner_worker.analysis"}, wantError: true},
		{
			name: "extra", rows: [][]any{valid, {"extra", "scanner_worker.warning", int64(7), "scanner_worker", false}},
			expected: map[string]string{"fact-analysis": "scanner_worker.analysis"}, wantError: true,
		},
		{
			name: "stale fence", rows: [][]any{{"fact-analysis", "scanner_worker.analysis", int64(8), "scanner_worker", false}},
			expected: map[string]string{"fact-analysis": "scanner_worker.analysis"}, wantError: true,
		},
		{
			// The analyzer kind in the claim identity is the #9407
			// violation: source_system must stay planner-owned.
			name: "wrong package producer", rows: [][]any{{"fact-warning", chain.VulnerabilityWarningFactKind, int64(7), ospackagevulnerability.CollectorKind, false}},
			expected: map[string]string{"fact-warning": chain.VulnerabilityWarningFactKind}, wantError: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fake := &scannerResultMembershipDB{rows: test.rows}
			err := writeScannerImageResult(context.Background(), fake, claim, receipt, test.expected)
			if test.wantError {
				if err == nil || len(fake.execQueries) != 0 {
					t.Fatalf("write result = %v, writes = %d; want refusal before receipt", err, len(fake.execQueries))
				}
				return
			}
			if err != nil || len(fake.requestedFactIDs) != len(test.expected) || len(fake.execQueries) != 2 ||
				fake.execQueries[0] != scannerResultReceiptInsertQuery ||
				fake.execQueries[1] != scannerResultFactAssociationsInsertQuery {
				t.Fatalf("write result = %v, writes = %d; want receipt then exact associations", err, len(fake.execQueries))
			}
		})
	}
}

func TestDerivedImageResultAcceptsPackageWarningProducer(t *testing.T) {
	t.Parallel()
	const scopeID = "image://derived/target"
	const generationID = "scan-generation"
	packageFacts, err := ospackagevulnerability.BuildEnvelopes(ospackagevulnerability.EnvelopeContext{
		ScopeID: scopeID, GenerationID: generationID, CollectorInstanceID: "scanner",
		FencingToken: 7, ObservedAt: time.Now().UTC(), SourceRecordID: "package-record",
	}, nil, []ospackagevulnerability.OSPackageWarning{{
		Reason: ospackagevulnerability.WarningReasonMalformedPackageRecord, RecordIndex: 1,
	}})
	if err != nil || len(packageFacts) != 1 {
		t.Fatalf("BuildEnvelopes() = %d facts, %v", len(packageFacts), err)
	}
	expected, err := validateDerivedImageResultFacts(workflow.ClaimMutation{FencingToken: 7},
		scopeID, generationID, packageFacts)
	if err != nil || expected[packageFacts[0].FactID] != chain.VulnerabilityWarningFactKind {
		t.Fatalf("package warning membership = %v, %v", expected, err)
	}
}

func TestDerivedIngestionPreclaimRunsBeforeClaimMutation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 29, 9, 0, 0, 0, time.UTC)
	database := &fakeTransactionalDB{tx: &fakeTx{}}
	store := NewIngestionStore(database)
	scopeValue, generation, mutation := stateWriterFixture(now)
	cause := errors.New("source authority revoked")
	called := false
	err := store.commitScopeGeneration(context.Background(), mutation, true,
		scopeValue, generation, nil, nil, nil,
		func(_ context.Context, tx db.ExecQueryer) error {
			called = true
			if tx != database.tx || len(database.tx.execs) != 0 {
				t.Fatal("preclaim hook ran after claim or fact write")
			}
			return cause
		}, nil)
	if !called || !errors.Is(err, cause) || !database.tx.rolledBack || database.tx.committed {
		t.Fatalf("preclaim refusal = %v, called=%t committed=%t rolledBack=%t",
			err, called, database.tx.committed, database.tx.rolledBack)
	}
	for _, exec := range database.tx.execs {
		if strings.Contains(exec.query, "workflow_claim") {
			t.Fatal("claim mutation ran after source authority refusal")
		}
	}
}
