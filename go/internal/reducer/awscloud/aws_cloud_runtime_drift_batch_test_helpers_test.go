// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
)

// Local copies of the reducer-root batched-insert and counter test helpers
// this family's tests used before the move (issue #6061). Go test files
// cannot share unexported symbols across a package boundary, so each helper
// the moved tests still need is duplicated here verbatim (against
// [factwrite]'s exported query/size constants rather than the root's own
// forwarders) rather than exported from the root for test-only use.

// decodedBatchedVersionedFactRow is one row recovered from a
// factwrite.BatchInsertVersionedQuery ExecContext call. Mirrors the root's
// decodedBatchedFactRow with an added SchemaVersion field, plus
// FencingToken (#5848).
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
// recorded by a fakeAWSCloudRuntimeDriftExecer into the per-row records they
// encode. It asserts each call used factwrite.BatchInsertVersionedQuery so a
// regression to a per-row versioned insert fails loudly here.
func decodeBatchedVersionedFactCalls(t *testing.T, calls []fakeAWSCloudRuntimeDriftExecCall) []decodedBatchedVersionedFactRow {
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
func decodeBatchedVersionedFactCall(t *testing.T, call fakeAWSCloudRuntimeDriftExecCall) []decodedBatchedVersionedFactRow {
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

// counterTotal sums every int64 counter data point named name across the
// collected resource metrics.
func counterTotal(rm metricdata.ResourceMetrics, name string) int64 {
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
		}
	}
	return total
}

// reducerCounterValue returns the value of the single int64 counter data
// point named metricName carrying exactly wantAttrs, failing the test if no
// such data point exists.
func reducerCounterValue(t *testing.T, rm metricdata.ResourceMetrics, metricName string, wantAttrs map[string]string) int64 {
	t.Helper()

	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != metricName {
				continue
			}

			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Sum[int64]", metricName, m.Data)
			}

			for _, dp := range sum.DataPoints {
				if hasAttrs(dp.Attributes.ToSlice(), wantAttrs) {
					return dp.Value
				}
			}
		}
	}

	t.Fatalf("metric %s with attrs %v not found", metricName, wantAttrs)
	return 0
}

func hasAttrs(actual []attribute.KeyValue, want map[string]string) bool {
	if len(actual) != len(want) {
		return false
	}

	for _, attr := range actual {
		if want[string(attr.Key)] != attr.Value.AsString() {
			return false
		}
	}

	return true
}
