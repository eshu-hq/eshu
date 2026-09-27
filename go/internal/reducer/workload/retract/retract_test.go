// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retract

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type recordedCall struct {
	cypher string
	params map[string]any
}

// recordingExecutor records statements and fails on the Nth call when set.
type recordingExecutor struct {
	calls     []recordedCall
	errOnCall int
	err       error
}

func (r *recordingExecutor) ExecuteCypher(_ context.Context, cypher string, params map[string]any) error {
	r.calls = append(r.calls, recordedCall{cypher: cypher, params: params})
	if r.errOnCall > 0 && len(r.calls) == r.errOnCall {
		return r.err
	}
	return nil
}

// countingExecutor reports a per-statement delete count, or ErrUncounted.
type countingExecutor struct {
	recordingExecutor
	deleted   int64
	uncounted bool
}

func (c *countingExecutor) ExecuteCypherCountingRelationshipDeletes(
	ctx context.Context, cypher string, params map[string]any,
) (int64, error) {
	if err := c.ExecuteCypher(ctx, cypher, params); err != nil {
		return 0, err
	}
	if c.uncounted {
		return 0, ErrUncounted
	}
	return c.deleted, nil
}

func repositoryFact(repoID string, delta bool) facts.Envelope {
	payload := map[string]any{"graph_id": repoID}
	if delta {
		payload["delta_generation"] = true
	}
	return facts.Envelope{FactKind: "repository", Payload: payload}
}

func TestFullGenerationRepositoryIDsExcludesDeltaRepositories(t *testing.T) {
	t.Parallel()

	got := FullGenerationRepositoryIDs([]facts.Envelope{
		repositoryFact("repository:b", false),
		repositoryFact("repository:a", false),
		repositoryFact("repository:d", false),
		repositoryFact("repository:d", true), // any delta fact excludes the repository
		repositoryFact("repository:c", true),
		repositoryFact(" ", false),
		{FactKind: "file", Payload: map[string]any{"graph_id": "repository:file"}},
	})
	if want := []string{"repository:a", "repository:b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("FullGenerationRepositoryIDs = %v, want %v", got, want)
	}
	if got := FullGenerationRepositoryIDs(nil); len(got) != 0 {
		t.Fatalf("no repository facts = %v, want none (no positive evidence, no retract)", got)
	}
}

func TestRepositoryEdgesCountsDeletesPerBatch(t *testing.T) {
	t.Parallel()

	exec := &countingExecutor{deleted: 2}
	keepLists := KeepLists([]string{"repository:a", "repository:b"},
		map[string][]string{"repository:a": {"workload:a", "workload:a"}}, nil)
	result, err := RepositoryEdges(context.Background(), exec, 1, keepLists, "finalization/workloads")
	if err != nil {
		t.Fatalf("RepositoryEdges() error = %v", err)
	}
	// Two repositories at batch size 1: two DEFINES and two EXPOSES_ENDPOINT
	// statements, each reporting 2 deletes.
	if !result.Counted || result.Repositories != 2 || result.DefinesDeleted != 4 || result.EndpointEdgesDeleted != 4 {
		t.Fatalf("result = %+v, want counted, 2 repositories, 4 + 4 deletes", result)
	}
	if len(exec.calls) != 4 {
		t.Fatalf("statements = %d, want 4", len(exec.calls))
	}
	for i, call := range exec.calls {
		rows := call.params["rows"].([]map[string]any)
		if keep, ok := rows[0]["keep_workload_ids"].([]string); !ok || keep == nil {
			t.Fatalf("call %d keep_workload_ids = %#v, want a non-nil list", i, rows[0]["keep_workload_ids"])
		}
		if keep, ok := rows[0]["keep_endpoint_ids"].([]string); !ok || keep == nil {
			t.Fatalf("call %d keep_endpoint_ids = %#v, want a non-nil list", i, rows[0]["keep_endpoint_ids"])
		}
		if call.params["evidence_source"] != "finalization/workloads" {
			t.Fatalf("call %d evidence_source = %#v", i, call.params["evidence_source"])
		}
	}
	if got := exec.calls[0].params["rows"].([]map[string]any)[0]["keep_workload_ids"]; !reflect.DeepEqual(got, []string{"workload:a"}) {
		t.Fatalf("deduplicated keep list = %#v, want [workload:a]", got)
	}
	for i, shape := range []string{"-[rel:DEFINES]->(w:Workload)", "-[rel:DEFINES]->(w:Workload)", "-[rel:EXPOSES_ENDPOINT]->(e:Endpoint)"} {
		if !strings.Contains(exec.calls[i].cypher, shape) || !strings.Contains(exec.calls[i].cypher, "MATCH (repo:Repository {id: row.repo_id})") {
			t.Fatalf("call %d cypher = %q, want the id-anchored %s retract", i, exec.calls[i].cypher, shape)
		}
	}
}

func TestRepositoryEdgesReportsUncountedChains(t *testing.T) {
	t.Parallel()

	for name, exec := range map[string]Executor{
		"no counting capability": &recordingExecutor{},
		"uncounted sentinel":     &countingExecutor{deleted: 9, uncounted: true},
	} {
		result, err := RepositoryEdges(context.Background(), exec, 500, []KeepList{{RepoID: "repository:a"}}, "src")
		if err != nil {
			t.Fatalf("%s: error = %v, want the write to succeed", name, err)
		}
		if result.Counted || result.DefinesDeleted != 0 || result.EndpointEdgesDeleted != 0 {
			t.Fatalf("%s: result = %+v, want uncounted with zero deletes", name, result)
		}
	}
}

func TestRepositoryEdgesValidatesInputs(t *testing.T) {
	t.Parallel()

	keep := []KeepList{{RepoID: "repository:a"}}
	if result, err := RepositoryEdges(context.Background(), nil, 500, nil, "src"); err != nil || result.Repositories != 0 {
		t.Fatalf("empty keep-lists = %+v, %v; want a no-op", result, err)
	}
	cases := map[string]struct {
		exec   Executor
		keep   []KeepList
		source string
		want   string
	}{
		"nil executor":       {nil, keep, "src", "requires an executor"},
		"blank source":       {&recordingExecutor{}, keep, " ", "requires an evidence source"},
		"blank repository":   {&recordingExecutor{}, []KeepList{{}}, "src", "requires a repository id"},
		"defines failure":    {&recordingExecutor{errOnCall: 1, err: errors.New("boom")}, keep, "src", "retract stale workload defines edges: boom"},
		"endpoint statement": {&recordingExecutor{errOnCall: 2, err: errors.New("late")}, keep, "src", "retract stale repository endpoint edges: late"},
	}
	for name, tc := range cases {
		_, err := RepositoryEdges(context.Background(), tc.exec, 500, tc.keep, tc.source)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: error = %v, want %q", name, err, tc.want)
		}
	}
}

func TestObserveRecordsMeasuredDeletesOnly(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	keep := []KeepList{{RepoID: "repository:a"}}
	Observe(context.Background(), inst, "scope", "gen", keep, Result{Repositories: 1, DefinesDeleted: 3, EndpointEdgesDeleted: 2, Counted: true}, 0)
	Observe(context.Background(), inst, "scope", "gen", keep, Result{Repositories: 1, DefinesDeleted: 7}, 0) // uncounted: not recorded
	Observe(context.Background(), nil, "scope", "gen", keep, Result{Counted: true, DefinesDeleted: 1}, 0)    // nil instruments

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	got := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_reconciliation_drift_retractions_total" {
				continue
			}
			for _, point := range m.Data.(metricdata.Sum[int64]).DataPoints {
				domain, _ := point.Attributes.Value(telemetry.MetricDimensionDomain)
				phase, _ := point.Attributes.Value(telemetry.MetricDimensionWritePhase)
				kind, _ := point.Attributes.Value(telemetry.MetricDimensionKind)
				got[domain.AsString()+"/"+phase.AsString()+"/"+kind.AsString()] = point.Value
			}
		}
	}
	want := map[string]int64{
		MetricDomain + "/" + PhaseDefines + "/edge":            3,
		MetricDomain + "/" + PhaseRepositoryEndpoint + "/edge": 2,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("recorded retractions = %v, want %v", got, want)
	}
}
