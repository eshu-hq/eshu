// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workloadid"
)

type fakeSiblingLookup struct {
	siblings []WorkloadSibling
	names    []string
	err      error
}

func (f *fakeSiblingLookup) ListWorkloadSiblings(
	_ context.Context,
	names []string,
) ([]WorkloadSibling, error) {
	f.names = append([]string(nil), names...)
	return f.siblings, f.err
}

func sameNameProjection() *ProjectionResult {
	return &ProjectionResult{
		ScopeID:      "scope-alpha",
		GenerationID: "gen-1",
		WorkloadRows: []WorkloadRow{{
			RepoID:         "repository:r_aaaaaaaa",
			WorkloadID:     "workload:repository:r_aaaaaaaa:checkout",
			WorkloadKind:   "service",
			WorkloadName:   "checkout",
			Classification: "service",
			Confidence:     0.97,
		}},
		InstanceRows: []InstanceRow{{
			Environment:  "prod",
			InstanceID:   "workload-instance:repository:r_aaaaaaaa:checkout:prod",
			RepoID:       "repository:r_aaaaaaaa",
			WorkloadID:   "workload:repository:r_aaaaaaaa:checkout",
			WorkloadName: "checkout",
			Confidence:   0.97,
		}},
		EndpointRows: []APIEndpointRow{{
			EndpointID:   "endpoint:abc",
			RepoID:       "repository:r_aaaaaaaa",
			WorkloadID:   "workload:repository:r_aaaaaaaa:checkout",
			WorkloadName: "checkout",
			Path:         "/pay",
		}},
	}
}

func TestMaterializeStampsScopeAndGeneration(t *testing.T) {
	t.Parallel()
	executor := &fakeNeo4jExecutor{}
	m := NewWorkloadMaterializer(executor)
	if _, err := m.Materialize(context.Background(), sameNameProjection()); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	for _, want := range []string{
		"w.scope_id = row.scope_id",
		"w.generation_id = row.generation_id",
		"i.scope_id = row.scope_id",
		"endpoint.scope_id = row.scope_id",
	} {
		if !containsCypher(executor.calls, want) {
			t.Fatalf("missing stamp cypher %q", want)
		}
	}
	rows := executor.calls[0].Parameters["rows"].([]map[string]any)
	if rows[0]["scope_id"] != "scope-alpha" || rows[0]["generation_id"] != "gen-1" {
		t.Fatalf("workload row stamps = %v/%v", rows[0]["scope_id"], rows[0]["generation_id"])
	}
}

func TestMaterializeWritesSameNameEdgeForCrossRepoSibling(t *testing.T) {
	t.Parallel()
	executor := &fakeNeo4jExecutor{}
	m := NewWorkloadMaterializer(executor)
	lookup := &fakeSiblingLookup{siblings: []WorkloadSibling{
		{ID: "workload:repository:r_bbbbbbbb:checkout", Name: "checkout", RepoID: "repository:r_bbbbbbbb"},
		// Self echo from the graph read must not self-pair.
		{ID: "workload:repository:r_aaaaaaaa:checkout", Name: "checkout", RepoID: "repository:r_aaaaaaaa"},
		// Same-repo duplicate id under another name must not pair.
		{ID: "workload:repository:r_aaaaaaaa:other", Name: "other", RepoID: "repository:r_aaaaaaaa"},
	}}
	m.SiblingLookup = lookup
	result, err := m.Materialize(context.Background(), sameNameProjection())
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if result.SameNameEdgesWritten != 1 {
		t.Fatalf("SameNameEdgesWritten = %d, want 1", result.SameNameEdgesWritten)
	}
	var pairRows []map[string]any
	for _, call := range executor.calls {
		if strings.Contains(call.Cypher, "MERGE (a)-[rel:SAME_NAME]->(b)") {
			pairRows = call.Parameters["rows"].([]map[string]any)
		}
	}
	if len(pairRows) != 1 {
		t.Fatalf("SAME_NAME pair rows = %d, want 1", len(pairRows))
	}
	row := pairRows[0]
	if row["a"] != "workload:repository:r_aaaaaaaa:checkout" ||
		row["b"] != "workload:repository:r_bbbbbbbb:checkout" {
		t.Fatalf("pair order = %v -> %v, want lexically smaller first", row["a"], row["b"])
	}
	if row["name"] != "checkout" || row["scope_id"] != "scope-alpha" {
		t.Fatalf("pair row = %v", row)
	}
	if len(lookup.names) != 1 || lookup.names[0] != "checkout" {
		t.Fatalf("lookup names = %v", lookup.names)
	}
}

func TestMaterializeSkipsSameNameWithoutLookup(t *testing.T) {
	t.Parallel()
	executor := &fakeNeo4jExecutor{}
	m := NewWorkloadMaterializer(executor)
	result, err := m.Materialize(context.Background(), sameNameProjection())
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if result.SameNameEdgesWritten != 0 {
		t.Fatalf("SameNameEdgesWritten = %d, want 0", result.SameNameEdgesWritten)
	}
	if containsCypher(executor.calls, "SAME_NAME") {
		t.Fatal("SAME_NAME cypher executed without lookup")
	}
}

// The handler's rejection tally must reach eshu_dp_workload_identity_rejected_total
// with one point per non-empty reason: a drop is counted, never silent (#5385).
// Zero-count reasons emit no point, and a nil instrument set is a no-op.
func TestRecordWorkloadIdentityRejectionsEmitsByReason(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	tally := workloadid.RejectionTally{BlankSegment: 2, ColonInName: 1}
	recordWorkloadIdentityRejections(context.Background(), instruments, tally)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	got := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_workload_identity_rejected_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("rejected_total data = %T, want Sum[int64]", m.Data)
			}
			for _, point := range sum.DataPoints {
				reason, _ := point.Attributes.Value(attribute.Key("reason"))
				got[string(reason.AsString())] += point.Value
			}
		}
	}
	if got["blank_segment"] != 2 {
		t.Errorf("reason blank_segment = %d, want 2", got["blank_segment"])
	}
	if got["colon_in_name"] != 1 {
		t.Errorf("reason colon_in_name = %d, want 1", got["colon_in_name"])
	}
	if _, ok := got["colon_in_environment"]; ok {
		t.Errorf("reason colon_in_environment emitted with zero count: must emit no point")
	}
}

func TestRecordWorkloadIdentityRejectionsNilSafe(t *testing.T) {
	t.Parallel()
	recordWorkloadIdentityRejections(context.Background(), nil, workloadid.RejectionTally{BlankSegment: 1})
	recordWorkloadIdentityRejections(context.Background(), &telemetry.Instruments{}, workloadid.RejectionTally{BlankSegment: 1})
}
