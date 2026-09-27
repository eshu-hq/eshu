// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/workload/retract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workloadid"
)

// The #7285 stale-edge retract (ruling B1). Once the projector stops
// DETACH DELETE-ing the Repository node on every non-delta attempt, nothing
// else removes a DEFINES or repository-side EXPOSES_ENDPOINT edge whose
// workload or endpoint is no longer a candidate. workload_materialization
// must retract them itself, provenance-scoped, for full generations only.

const (
	repoEdgeRetractRepoID    = "repository:r_payments"
	repoEdgeRetractOtherRepo = "repository:r_ledger"
	retractDefinesShape      = "-[rel:DEFINES]->(w:Workload)"
	retractEndpointShape     = "-[rel:EXPOSES_ENDPOINT]->(e:Endpoint)"
)

func repoEdgeRetractRepositoryFact(repoID string, delta bool) facts.Envelope {
	payload := map[string]any{"graph_id": repoID, "name": repoID}
	if delta {
		payload["delta_generation"] = true
		payload["delta_relative_paths"] = []any{"cmd/main.go"}
	}
	return facts.Envelope{FactID: "fact-" + repoID, FactKind: "repository", Payload: payload, ObservedAt: time.Now().UTC()}
}

func repoEdgeRetractCandidate(repoID, name string) WorkloadCandidate {
	return WorkloadCandidate{
		RepoID: repoID, RepoName: name, WorkloadName: name,
		Classification: "service", Confidence: 0.95,
		APIEndpoints: []APIEndpointSignal{{Path: "/v1/" + name, Methods: []string{"GET"}}},
	}
}

func repoEdgeRetractTestIntent() Intent {
	now := time.Now().UTC()
	return Intent{
		IntentID: "intent-wm-7285", ScopeID: "scope-payments", GenerationID: "gen-2",
		SourceSystem: "git", Domain: DomainWorkloadMaterialization, Cause: "facts projected",
		EntityKeys: []string{repoEdgeRetractRepoID}, RelatedScopeIDs: []string{"scope-payments"},
		EnqueuedAt: now, AvailableAt: now, Status: IntentStatusPending,
	}
}

func runRepoEdgeRetractHandler(
	t *testing.T,
	repoFacts []facts.Envelope,
	candidates []WorkloadCandidate,
) *recordingCypherExecutor {
	t.Helper()
	executor := &recordingCypherExecutor{}
	handler := WorkloadMaterializationHandler{
		FactLoader:   &stubFactLoader{envelopes: repoFacts},
		InputLoader:  &stubWorkloadProjectionInputLoader{candidates: candidates},
		Materializer: NewWorkloadMaterializer(executor),
	}
	if _, err := handler.Handle(context.Background(), repoEdgeRetractTestIntent()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	return executor
}

// retractCall returns the index and parameters of the single recorded
// statement matching shape followed by DELETE rel, or -1.
func repoEdgeRetractCall(t *testing.T, calls []recordedCypherCall, shape string) (int, map[string]any) {
	t.Helper()
	index := -1
	var params map[string]any
	for i, call := range calls {
		if strings.Contains(call.cypher, shape) && strings.Contains(call.cypher, "DELETE rel") {
			if index >= 0 {
				t.Fatalf("more than one %s retract statement", shape)
			}
			index, params = i, call.params
		}
	}
	return index, params
}

func repoEdgeRetractRows(t *testing.T, params map[string]any) []map[string]any {
	t.Helper()
	rows, ok := params["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("retract rows = %#v, want []map[string]any", params["rows"])
	}
	return rows
}

func TestWorkloadMaterializationRetractsStaleRepositoryEdgesOnFullGeneration(t *testing.T) {
	t.Parallel()

	executor := runRepoEdgeRetractHandler(t,
		[]facts.Envelope{repoEdgeRetractRepositoryFact(repoEdgeRetractRepoID, false)},
		[]WorkloadCandidate{repoEdgeRetractCandidate(repoEdgeRetractRepoID, "payments")},
	)
	workloadID := workloadid.NewWorkloadID(repoEdgeRetractRepoID, "payments").String()
	endpointID := stableAPIEndpointID(repoEdgeRetractRepoID, workloadID, "/v1/payments")

	upsertIndex := -1
	for i, call := range executor.calls {
		if strings.Contains(call.cypher, "MERGE (repo)-[rel:DEFINES]->(w)") {
			upsertIndex = i
		}
	}
	definesIndex, definesParams := repoEdgeRetractCall(t, executor.calls, retractDefinesShape)
	endpointIndex, endpointParams := repoEdgeRetractCall(t, executor.calls, retractEndpointShape)
	if definesIndex < 0 || endpointIndex < 0 {
		t.Fatalf("stale retract missing: defines=%d endpoint=%d; full generations must retract stale DEFINES and EXPOSES_ENDPOINT", definesIndex, endpointIndex)
	}
	if upsertIndex < 0 || definesIndex < upsertIndex || endpointIndex < upsertIndex {
		t.Fatalf("retract ran at defines=%d endpoint=%d before the DEFINES upsert at %d; it must follow the durable replacement", definesIndex, endpointIndex, upsertIndex)
	}
	for _, params := range []map[string]any{definesParams, endpointParams} {
		if got := params["evidence_source"]; got != EvidenceSourceWorkloads {
			t.Fatalf("retract evidence_source = %#v, want %q", got, EvidenceSourceWorkloads)
		}
	}
	want := []map[string]any{{
		"repo_id":           repoEdgeRetractRepoID,
		"keep_workload_ids": []string{workloadID},
		"keep_endpoint_ids": []string{endpointID},
	}}
	if got := repoEdgeRetractRows(t, definesParams); !reflect.DeepEqual(got, want) {
		t.Fatalf("DEFINES retract rows = %#v, want %#v", got, want)
	}
	if got := repoEdgeRetractRows(t, endpointParams); !reflect.DeepEqual(got, want) {
		t.Fatalf("EXPOSES_ENDPOINT retract rows = %#v, want %#v", got, want)
	}
}

func TestWorkloadMaterializationRetractsAllOwnRepositoryEdgesWhenNoCandidates(t *testing.T) {
	t.Parallel()

	executor := runRepoEdgeRetractHandler(t,
		[]facts.Envelope{repoEdgeRetractRepositoryFact(repoEdgeRetractRepoID, false)},
		nil,
	)
	_, definesParams := repoEdgeRetractCall(t, executor.calls, retractDefinesShape)
	_, endpointParams := repoEdgeRetractCall(t, executor.calls, retractEndpointShape)
	if definesParams == nil || endpointParams == nil {
		t.Fatal("zero-candidate full generation did not retract; a repository that lost every workload keeps stale DEFINES")
	}
	// Explicit empty keep lists: a missing or null keep-list makes
	// `NOT (w.id IN null)` null, which would silently delete nothing.
	want := []map[string]any{{
		"repo_id":           repoEdgeRetractRepoID,
		"keep_workload_ids": []string{},
		"keep_endpoint_ids": []string{},
	}}
	if got := repoEdgeRetractRows(t, definesParams); !reflect.DeepEqual(got, want) {
		t.Fatalf("zero-candidate DEFINES retract rows = %#v, want %#v", got, want)
	}
	if got := repoEdgeRetractRows(t, endpointParams); !reflect.DeepEqual(got, want) {
		t.Fatalf("zero-candidate EXPOSES_ENDPOINT retract rows = %#v, want %#v", got, want)
	}
}

func TestWorkloadMaterializationSkipsRepositoryEdgeRetractOnDeltaGeneration(t *testing.T) {
	t.Parallel()

	for name, candidates := range map[string][]WorkloadCandidate{
		"with candidates": {repoEdgeRetractCandidate(repoEdgeRetractRepoID, "payments")},
		"zero candidates": nil,
	} {
		executor := runRepoEdgeRetractHandler(t,
			[]facts.Envelope{repoEdgeRetractRepositoryFact(repoEdgeRetractRepoID, true)},
			candidates,
		)
		for _, shape := range []string{retractDefinesShape, retractEndpointShape} {
			if index, _ := repoEdgeRetractCall(t, executor.calls, shape); index >= 0 {
				t.Fatalf("%s: delta generation retracted %s; a delta reads partial facts and must never retract", name, shape)
			}
		}
	}
}

func TestWorkloadMaterializationRetractsOnlyFullRepositoriesInMixedScope(t *testing.T) {
	t.Parallel()

	executor := runRepoEdgeRetractHandler(t,
		[]facts.Envelope{
			repoEdgeRetractRepositoryFact(repoEdgeRetractRepoID, false),
			repoEdgeRetractRepositoryFact(repoEdgeRetractOtherRepo, true),
		},
		[]WorkloadCandidate{
			repoEdgeRetractCandidate(repoEdgeRetractRepoID, "payments"),
			repoEdgeRetractCandidate(repoEdgeRetractOtherRepo, "ledger"),
		},
	)
	_, params := repoEdgeRetractCall(t, executor.calls, retractDefinesShape)
	if params == nil {
		t.Fatal("full repository in a mixed scope was not retracted")
	}
	rows := repoEdgeRetractRows(t, params)
	if len(rows) != 1 || rows[0]["repo_id"] != repoEdgeRetractRepoID {
		t.Fatalf("mixed-scope retract rows = %#v, want only %s (the delta repository is excluded)", rows, repoEdgeRetractRepoID)
	}
}

type failingRepoEdgeRetractExecutor struct {
	recordingCypherExecutor
}

func (f *failingRepoEdgeRetractExecutor) ExecuteCypher(ctx context.Context, cypher string, params map[string]any) error {
	if strings.Contains(cypher, retractDefinesShape) && strings.Contains(cypher, "DELETE rel") {
		return errors.New("graph unavailable")
	}
	return f.recordingCypherExecutor.ExecuteCypher(ctx, cypher, params)
}

func TestWorkloadMaterializationRetractFailureFailsTheIntent(t *testing.T) {
	t.Parallel()

	handler := WorkloadMaterializationHandler{
		FactLoader:   &stubFactLoader{envelopes: []facts.Envelope{repoEdgeRetractRepositoryFact(repoEdgeRetractRepoID, false)}},
		InputLoader:  &stubWorkloadProjectionInputLoader{candidates: []WorkloadCandidate{repoEdgeRetractCandidate(repoEdgeRetractRepoID, "payments")}},
		Materializer: NewWorkloadMaterializer(&failingRepoEdgeRetractExecutor{}),
	}
	_, err := handler.Handle(context.Background(), repoEdgeRetractTestIntent())
	if err == nil || !strings.Contains(err.Error(), "graph unavailable") {
		t.Fatalf("Handle() error = %v, want the retract failure so the intent retries", err)
	}
}

// countingRetractExecutor reports a per-statement delete count, or the
// uncounted sentinel, through retract.CountingExecutor.
type countingRetractExecutor struct {
	fakeNeo4jExecutor
	deleted   int64
	uncounted bool
}

func (c *countingRetractExecutor) ExecuteCypherCountingRelationshipDeletes(
	ctx context.Context, cypher string, params map[string]any,
) (int64, error) {
	if err := c.ExecuteCypher(ctx, cypher, params); err != nil {
		return 0, err
	}
	if c.uncounted {
		return 0, retract.ErrUncounted
	}
	return c.deleted, nil
}

// TestWorkloadMaterializationRecordsRepositoryEdgeRetractCounts pins the
// operator signal: actual deleted edges land on
// eshu_dp_reconciliation_drift_retractions_total under the bounded
// workload_materialization domain, one write_phase per edge family.
func TestWorkloadMaterializationRecordsRepositoryEdgeRetractCounts(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	handler := WorkloadMaterializationHandler{
		FactLoader:   &stubFactLoader{envelopes: []facts.Envelope{repoEdgeRetractRepositoryFact(repoEdgeRetractRepoID, false)}},
		InputLoader:  &stubWorkloadProjectionInputLoader{},
		Materializer: NewWorkloadMaterializer(&countingRetractExecutor{deleted: 3}),
		Instruments:  inst,
	}
	if _, err := handler.Handle(context.Background(), repoEdgeRetractTestIntent()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, phase := range []string{retract.PhaseDefines, retract.PhaseRepositoryEndpoint} {
		attrs := map[string]string{
			telemetry.MetricDimensionDomain:     retract.MetricDomain,
			telemetry.MetricDimensionWritePhase: phase,
			telemetry.MetricDimensionKind:       "edge",
		}
		if got := reducerCounterValue(t, rm, "eshu_dp_reconciliation_drift_retractions_total", attrs); got != 3 {
			t.Fatalf("%s retractions = %d, want 3", phase, got)
		}
	}
}
