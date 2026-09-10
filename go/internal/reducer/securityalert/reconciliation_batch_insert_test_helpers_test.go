// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalert

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
)

// This file is a local copy of the reducer root's
// reducer_fact_batch_insert_test_helpers_test.go, trimmed to the pieces this
// package's tests use. Go test files cannot share unexported symbols across a
// package boundary, and fakeWorkloadIdentityExecer plus the batched-call
// decoders are unexported test doubles several still-in-root families also
// depend on from their own root copy, and [servicecatalog]/[containerimage]
// each keep their own trimmed copy for the same reason (issue #6061).

// fakeWorkloadIdentityExecer records every ExecContext call so a batched
// writer test can decode and assert on the rows it issued.
type fakeWorkloadIdentityExecer struct {
	execs []fakeWorkloadIdentityExecCall
}

type fakeWorkloadIdentityExecCall struct {
	query string
	args  []any
}

func (f *fakeWorkloadIdentityExecer) ExecContext(
	_ context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	f.execs = append(f.execs, fakeWorkloadIdentityExecCall{query: query, args: args})
	return fakeWorkloadIdentityResult{}, nil
}

type fakeWorkloadIdentityResult struct{}

func (fakeWorkloadIdentityResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeWorkloadIdentityResult) RowsAffected() (int64, error) { return 1, nil }

// decodedBatchedFactRow is one row recovered from a factwrite.BatchInsertQuery
// ExecContext call. Tests assert on the decoded rows so a batched writer can be
// checked the same way a per-row writer was: by fact_id, fact_kind, payload, etc.
type decodedBatchedFactRow struct {
	FactID           string
	ScopeID          string
	GenerationID     string
	FactKind         string
	StableFactKey    string
	CollectorKind    string
	SourceConfidence string
	SourceSystem     string
	SourceFactKey    string
	SourceURI        *string
	SourceRecordID   *string
	ObservedAt       time.Time
	IngestedAt       time.Time
	IsTombstone      bool
	Payload          []byte
	FencingToken     int64
}

// decodeBatchedFactCalls flattens every batched ExecContext call recorded by a
// fakeWorkloadIdentityExecer into the per-row records they encode. It asserts
// each call used factwrite.BatchInsertQuery so a regression to per-row inserts
// fails loudly here.
func decodeBatchedFactCalls(t *testing.T, calls []fakeWorkloadIdentityExecCall) []decodedBatchedFactRow {
	t.Helper()
	var rows []decodedBatchedFactRow
	for callIndex, call := range calls {
		if call.query != factwrite.BatchInsertQuery {
			t.Fatalf("exec %d query = %q, want batched fact insert", callIndex, call.query)
		}
		rows = append(rows, decodeBatchedFactCall(t, call)...)
	}
	return rows
}

// decodeBatchedFactCall decodes the parallel array arguments of a single batched
// insert call back into per-row records.
func decodeBatchedFactCall(t *testing.T, call fakeWorkloadIdentityExecCall) []decodedBatchedFactRow {
	t.Helper()
	if len(call.args) != 16 {
		t.Fatalf("batched insert args = %d, want 16", len(call.args))
	}
	factIDs := stringArg(t, call.args[0], "fact_id")
	scopeIDs := stringArg(t, call.args[1], "scope_id")
	generationIDs := stringArg(t, call.args[2], "generation_id")
	factKinds := stringArg(t, call.args[3], "fact_kind")
	stableKeys := stringArg(t, call.args[4], "stable_fact_key")
	collectorKinds := stringArg(t, call.args[5], "collector_kind")
	sourceConfidences := stringArg(t, call.args[6], "source_confidence")
	sourceSystems := stringArg(t, call.args[7], "source_system")
	sourceFactKeys := stringArg(t, call.args[8], "source_fact_key")
	sourceURIs := stringPtrArg(t, call.args[9], "source_uri")
	sourceRecordIDs := stringPtrArg(t, call.args[10], "source_record_id")
	observedAts := timeArg(t, call.args[11], "observed_at")
	ingestedAts := timeArg(t, call.args[12], "ingested_at")
	isTombstones := boolArg(t, call.args[13], "is_tombstone")
	payloads := stringArg(t, call.args[14], "payload")
	fencingTokens := int64Arg(t, call.args[15], "fencing_token")

	n := len(factIDs)
	rows := make([]decodedBatchedFactRow, n)
	for i := 0; i < n; i++ {
		rows[i] = decodedBatchedFactRow{
			FactID:           factIDs[i],
			ScopeID:          scopeIDs[i],
			GenerationID:     generationIDs[i],
			FactKind:         factKinds[i],
			StableFactKey:    stableKeys[i],
			CollectorKind:    collectorKinds[i],
			SourceConfidence: sourceConfidences[i],
			SourceSystem:     sourceSystems[i],
			SourceFactKey:    sourceFactKeys[i],
			SourceURI:        sourceURIs[i],
			SourceRecordID:   sourceRecordIDs[i],
			ObservedAt:       observedAts[i],
			IngestedAt:       ingestedAts[i],
			IsTombstone:      isTombstones[i],
			Payload:          []byte(payloads[i]),
			FencingToken:     fencingTokens[i],
		}
	}
	return rows
}

// expectedBatchedExecCount returns the number of ExecContext calls a batched
// writer must issue for rowCount rows: ceil(rowCount/factwrite.BatchSize).
func expectedBatchedExecCount(rowCount int) int {
	if rowCount == 0 {
		return 0
	}
	return (rowCount + factwrite.BatchSize - 1) / factwrite.BatchSize
}

func stringArg(t *testing.T, arg any, name string) []string {
	t.Helper()
	values, ok := arg.([]string)
	if !ok {
		t.Fatalf("%s arg type = %T, want []string", name, arg)
	}
	return values
}

func stringPtrArg(t *testing.T, arg any, name string) []*string {
	t.Helper()
	values, ok := arg.([]*string)
	if !ok {
		t.Fatalf("%s arg type = %T, want []*string", name, arg)
	}
	return values
}

func timeArg(t *testing.T, arg any, name string) []time.Time {
	t.Helper()
	values, ok := arg.([]time.Time)
	if !ok {
		t.Fatalf("%s arg type = %T, want []time.Time", name, arg)
	}
	return values
}

func int64Arg(t *testing.T, arg any, name string) []int64 {
	t.Helper()
	values, ok := arg.([]int64)
	if !ok {
		t.Fatalf("%s arg type = %T, want []int64", name, arg)
	}
	return values
}

func boolArg(t *testing.T, arg any, name string) []bool {
	t.Helper()
	values, ok := arg.([]bool)
	if !ok {
		t.Fatalf("%s arg type = %T, want []bool", name, arg)
	}
	return values
}
