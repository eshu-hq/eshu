// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factwritetest

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
)

// BatchedFactRow is one row recovered from a [factwrite.BatchInsertQuery]
// ExecContext call. Tests assert on the decoded rows so a batched writer can
// be checked the same way a per-row writer was: by fact_id, fact_kind,
// payload, etc.
type BatchedFactRow struct {
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

// DecodeBatchedFactCalls flattens every batched ExecContext call recorded by
// a [FakeExecer] into the per-row records they encode. It asserts each call
// used [factwrite.BatchInsertQuery] so a regression to per-row inserts fails
// loudly here.
func DecodeBatchedFactCalls(t testing.TB, calls []ExecCall) []BatchedFactRow {
	t.Helper()
	var rows []BatchedFactRow
	for callIndex, call := range calls {
		if call.Query != factwrite.BatchInsertQuery {
			t.Fatalf("exec %d query = %q, want batched fact insert", callIndex, call.Query)
		}
		rows = append(rows, decodeBatchedFactCall(t, call)...)
	}
	return rows
}

// decodeBatchedFactCall decodes the parallel array arguments of a single
// batched insert call back into per-row records, in the column order
// [factwrite.ChunkArgs] flattens them.
func decodeBatchedFactCall(t testing.TB, call ExecCall) []BatchedFactRow {
	t.Helper()
	if len(call.Args) != 16 {
		t.Fatalf("batched insert args = %d, want 16", len(call.Args))
	}
	factIDs := stringArg(t, call.Args[0], "fact_id")
	scopeIDs := stringArg(t, call.Args[1], "scope_id")
	generationIDs := stringArg(t, call.Args[2], "generation_id")
	factKinds := stringArg(t, call.Args[3], "fact_kind")
	stableKeys := stringArg(t, call.Args[4], "stable_fact_key")
	collectorKinds := stringArg(t, call.Args[5], "collector_kind")
	sourceConfidences := stringArg(t, call.Args[6], "source_confidence")
	sourceSystems := stringArg(t, call.Args[7], "source_system")
	sourceFactKeys := stringArg(t, call.Args[8], "source_fact_key")
	sourceURIs := stringPtrArg(t, call.Args[9], "source_uri")
	sourceRecordIDs := stringPtrArg(t, call.Args[10], "source_record_id")
	observedAts := timeArg(t, call.Args[11], "observed_at")
	ingestedAts := timeArg(t, call.Args[12], "ingested_at")
	isTombstones := boolArg(t, call.Args[13], "is_tombstone")
	payloads := stringArg(t, call.Args[14], "payload")
	fencingTokens := int64Arg(t, call.Args[15], "fencing_token")

	n := len(factIDs)
	rows := make([]BatchedFactRow, n)
	for i := 0; i < n; i++ {
		rows[i] = BatchedFactRow{
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

// ExpectedBatchedExecCount returns the number of ExecContext calls a batched
// writer must issue for rowCount rows: ceil(rowCount/[factwrite.BatchSize]).
func ExpectedBatchedExecCount(rowCount int) int {
	if rowCount == 0 {
		return 0
	}
	return (rowCount + factwrite.BatchSize - 1) / factwrite.BatchSize
}

func stringArg(t testing.TB, arg any, name string) []string {
	t.Helper()
	values, ok := arg.([]string)
	if !ok {
		t.Fatalf("%s arg type = %T, want []string", name, arg)
	}
	return values
}

func stringPtrArg(t testing.TB, arg any, name string) []*string {
	t.Helper()
	values, ok := arg.([]*string)
	if !ok {
		t.Fatalf("%s arg type = %T, want []*string", name, arg)
	}
	return values
}

func timeArg(t testing.TB, arg any, name string) []time.Time {
	t.Helper()
	values, ok := arg.([]time.Time)
	if !ok {
		t.Fatalf("%s arg type = %T, want []time.Time", name, arg)
	}
	return values
}

func int64Arg(t testing.TB, arg any, name string) []int64 {
	t.Helper()
	values, ok := arg.([]int64)
	if !ok {
		t.Fatalf("%s arg type = %T, want []int64", name, arg)
	}
	return values
}

func boolArg(t testing.TB, arg any, name string) []bool {
	t.Helper()
	values, ok := arg.([]bool)
	if !ok {
		t.Fatalf("%s arg type = %T, want []bool", name, arg)
	}
	return values
}
