// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tfconfigstate

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
)

// This file is a package-scoped copy of the fakeWorkloadIdentityExecer /
// decodeBatchedVersionedFactCalls test doubles the reducer root still keeps
// in workload_identity_writer_test.go and
// reducer_fact_batch_insert_test_helpers_test.go for the other batch-insert
// writer families that have not moved out of the root yet (issue #6061).
// Those root helpers are shared by 17 files across several other families
// (aws_cloud_runtime_drift, multi_cloud_runtime_drift, supply_chain_impact,
// workload_identity, package_correlation, cloud_inventory_admission); moving
// or exporting them was out of scope for this move-only split, so
// terraform_config_state_drift's tests carry their own copy scoped to only
// the versioned-insert shape they use, wired to the exported
// [factwrite.BatchInsertVersionedQuery] / [factwrite.BatchSize] leaves
// instead of the root's compat aliases.

// fakeWorkloadIdentityExecCall is one recorded ExecContext call.
type fakeWorkloadIdentityExecCall struct {
	query string
	args  []any
}

// fakeWorkloadIdentityExecer is a call-recording stand-in for
// [factwrite.Execer].
type fakeWorkloadIdentityExecer struct {
	execs []fakeWorkloadIdentityExecCall
}

func (f *fakeWorkloadIdentityExecer) ExecContext(
	_ context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	f.execs = append(f.execs, fakeWorkloadIdentityExecCall{query: query, args: args})
	return fakeWorkloadIdentityResult{}, nil
}

// fakeWorkloadIdentityResult is a no-op sql.Result.
type fakeWorkloadIdentityResult struct{}

func (fakeWorkloadIdentityResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeWorkloadIdentityResult) RowsAffected() (int64, error) { return 1, nil }

// expectedBatchedExecCount returns the number of ExecContext calls a batched
// writer must issue for rowCount rows: ceil(rowCount/factwrite.BatchSize).
func expectedBatchedExecCount(rowCount int) int {
	if rowCount == 0 {
		return 0
	}
	return (rowCount + factwrite.BatchSize - 1) / factwrite.BatchSize
}

// decodedBatchedVersionedFactRow is one row recovered from a
// factwrite.BatchInsertVersionedQuery ExecContext call.
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
	FencingToken     int64
}

// decodeBatchedVersionedFactCalls flattens every batched ExecContext call
// recorded by a fakeWorkloadIdentityExecer into the per-row records they
// encode. It asserts each call used factwrite.BatchInsertVersionedQuery so a
// regression to a per-row versioned insert fails loudly here.
func decodeBatchedVersionedFactCalls(t *testing.T, calls []fakeWorkloadIdentityExecCall) []decodedBatchedVersionedFactRow {
	t.Helper()
	var rows []decodedBatchedVersionedFactRow
	for callIndex, call := range calls {
		if call.query != factwrite.BatchInsertVersionedQuery {
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
	if len(call.args) != 17 {
		t.Fatalf("batched versioned insert args = %d, want 17", len(call.args))
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
	fencingTokens := int64Arg(t, call.args[16], "fencing_token")

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
			FencingToken:     fencingTokens[i],
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
