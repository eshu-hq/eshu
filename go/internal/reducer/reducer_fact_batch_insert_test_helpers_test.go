// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"
)

// decodedBatchedFactRow is one row recovered from a reducerFactBatchInsertQuery
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
}

// decodeBatchedFactCalls flattens every batched ExecContext call recorded by a
// fakeWorkloadIdentityExecer into the per-row records they encode. It asserts
// each call used reducerFactBatchInsertQuery so a regression to per-row inserts
// (which use canonicalReducerFactInsertQuery) fails loudly here.
func decodeBatchedFactCalls(t *testing.T, calls []fakeWorkloadIdentityExecCall) []decodedBatchedFactRow {
	t.Helper()
	var rows []decodedBatchedFactRow
	for callIndex, call := range calls {
		if call.query != reducerFactBatchInsertQuery {
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
	if len(call.args) != 15 {
		t.Fatalf("batched insert args = %d, want 15", len(call.args))
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
		}
	}
	return rows
}

// expectedBatchedExecCount returns the number of ExecContext calls a batched
// writer must issue for rowCount rows: ceil(rowCount/reducerFactBatchSize).
func expectedBatchedExecCount(rowCount int) int {
	if rowCount == 0 {
		return 0
	}
	return (rowCount + reducerFactBatchSize - 1) / reducerFactBatchSize
}

// decodedBatchedVersionedFactRow is one row recovered from a
// reducerFactBatchInsertVersionedQuery ExecContext call. It mirrors
// decodedBatchedFactRow with an added SchemaVersion field.
type decodedBatchedVersionedFactRow struct {
	FactID           string
	ScopeID          string
	GenerationID     string
	FactKind         string
	StableFactKey    string
	SchemaVersion    string
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
}

// decodeBatchedVersionedFactCalls flattens every batched ExecContext call
// recorded by a fakeWorkloadIdentityExecer into the per-row records they
// encode. It asserts each call used reducerFactBatchInsertVersionedQuery so a
// regression to a per-row versioned insert fails loudly here.
func decodeBatchedVersionedFactCalls(t *testing.T, calls []fakeWorkloadIdentityExecCall) []decodedBatchedVersionedFactRow {
	t.Helper()
	var rows []decodedBatchedVersionedFactRow
	for callIndex, call := range calls {
		if call.query != reducerFactBatchInsertVersionedQuery {
			t.Fatalf("exec %d query = %q, want batched versioned fact insert", callIndex, call.query)
		}
		rows = append(rows, decodeBatchedVersionedFactCall(t, call)...)
	}
	return rows
}

// decodeBatchedVersionedFactCall decodes the parallel array arguments of a
// single batched versioned insert call back into per-row records.
func decodeBatchedVersionedFactCall(t *testing.T, call fakeWorkloadIdentityExecCall) []decodedBatchedVersionedFactRow {
	t.Helper()
	if len(call.args) != 16 {
		t.Fatalf("batched versioned insert args = %d, want 16", len(call.args))
	}
	factIDs := stringArg(t, call.args[0], "fact_id")
	scopeIDs := stringArg(t, call.args[1], "scope_id")
	generationIDs := stringArg(t, call.args[2], "generation_id")
	factKinds := stringArg(t, call.args[3], "fact_kind")
	stableKeys := stringArg(t, call.args[4], "stable_fact_key")
	schemaVersions := stringArg(t, call.args[5], "schema_version")
	collectorKinds := stringArg(t, call.args[6], "collector_kind")
	sourceConfidences := stringArg(t, call.args[7], "source_confidence")
	sourceSystems := stringArg(t, call.args[8], "source_system")
	sourceFactKeys := stringArg(t, call.args[9], "source_fact_key")
	sourceURIs := stringPtrArg(t, call.args[10], "source_uri")
	sourceRecordIDs := stringPtrArg(t, call.args[11], "source_record_id")
	observedAts := timeArg(t, call.args[12], "observed_at")
	ingestedAts := timeArg(t, call.args[13], "ingested_at")
	isTombstones := boolArg(t, call.args[14], "is_tombstone")
	payloads := stringArg(t, call.args[15], "payload")

	n := len(factIDs)
	rows := make([]decodedBatchedVersionedFactRow, n)
	for i := 0; i < n; i++ {
		rows[i] = decodedBatchedVersionedFactRow{
			FactID:           factIDs[i],
			ScopeID:          scopeIDs[i],
			GenerationID:     generationIDs[i],
			FactKind:         factKinds[i],
			StableFactKey:    stableKeys[i],
			SchemaVersion:    schemaVersions[i],
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
		}
	}
	return rows
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

func boolArg(t *testing.T, arg any, name string) []bool {
	t.Helper()
	values, ok := arg.([]bool)
	if !ok {
		t.Fatalf("%s arg type = %T, want []bool", name, arg)
	}
	return values
}
