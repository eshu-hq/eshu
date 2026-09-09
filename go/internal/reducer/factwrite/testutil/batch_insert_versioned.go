// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package testutil

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
)

// VersionedBatchedFactRow is one row recovered from a
// [factwrite.BatchInsertVersionedQuery] ExecContext call. It mirrors
// [BatchedFactRow] with the added SchemaVersion the versioned insert carries.
// Tests assert on the decoded rows so a batched writer can be checked the
// same way a per-row writer was: by fact_id, fact_kind, payload, etc.
type VersionedBatchedFactRow struct {
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
	FencingToken     int64
}

// DecodeBatchedVersionedFactCalls flattens every batched ExecContext call
// recorded by a [FakeExecer] into the per-row records they encode. It asserts
// each call used [factwrite.BatchInsertVersionedQuery] so a regression to a
// per-row versioned insert fails loudly here.
func DecodeBatchedVersionedFactCalls(t testing.TB, calls []ExecCall) []VersionedBatchedFactRow {
	t.Helper()
	var rows []VersionedBatchedFactRow
	for callIndex, call := range calls {
		if call.Query != factwrite.BatchInsertVersionedQuery {
			t.Fatalf("exec %d query = %q, want batched versioned fact insert", callIndex, call.Query)
		}
		rows = append(rows, decodeBatchedVersionedFactCall(t, call)...)
	}
	return rows
}

// decodeBatchedVersionedFactCall decodes the parallel array arguments of a
// single batched versioned insert call back into per-row records, in the
// column order execVersionedChunk flattens them.
func decodeBatchedVersionedFactCall(t testing.TB, call ExecCall) []VersionedBatchedFactRow {
	t.Helper()
	if len(call.Args) != 17 {
		t.Fatalf("batched versioned insert args = %d, want 17", len(call.Args))
	}
	factIDs := stringArg(t, call.Args[0], "fact_id")
	scopeIDs := stringArg(t, call.Args[1], "scope_id")
	generationIDs := stringArg(t, call.Args[2], "generation_id")
	factKinds := stringArg(t, call.Args[3], "fact_kind")
	stableKeys := stringArg(t, call.Args[4], "stable_fact_key")
	schemaVersions := stringArg(t, call.Args[5], "schema_version")
	collectorKinds := stringArg(t, call.Args[6], "collector_kind")
	sourceConfidences := stringArg(t, call.Args[7], "source_confidence")
	sourceSystems := stringArg(t, call.Args[8], "source_system")
	sourceFactKeys := stringArg(t, call.Args[9], "source_fact_key")
	sourceURIs := stringPtrArg(t, call.Args[10], "source_uri")
	sourceRecordIDs := stringPtrArg(t, call.Args[11], "source_record_id")
	observedAts := timeArg(t, call.Args[12], "observed_at")
	ingestedAts := timeArg(t, call.Args[13], "ingested_at")
	isTombstones := boolArg(t, call.Args[14], "is_tombstone")
	payloads := stringArg(t, call.Args[15], "payload")
	fencingTokens := int64Arg(t, call.Args[16], "fencing_token")

	n := len(factIDs)
	rows := make([]VersionedBatchedFactRow, n)
	for i := 0; i < n; i++ {
		rows[i] = VersionedBatchedFactRow{
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
			FencingToken:     fencingTokens[i],
		}
	}
	return rows
}
