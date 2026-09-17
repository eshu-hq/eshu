// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// probeScriptMaterializerExecutor is a CypherExecutor fake with a scripted
// read-only existence probe. probePresent=false means at least one batch
// target is absent from the graph, so a guarded batch must not complete
// silently.
type probeScriptMaterializerExecutor struct {
	executedCyphers []string
	probeCalls      int
	probeCyphers    []string
	probePresent    bool
	probeErr        error
}

func (e *probeScriptMaterializerExecutor) ExecuteCypher(_ context.Context, cypher string, _ map[string]any) error {
	e.executedCyphers = append(e.executedCyphers, cypher)
	return nil
}

func (e *probeScriptMaterializerExecutor) ProbeGraphExists(_ context.Context, cypher string, _ map[string]any) (bool, error) {
	e.probeCalls++
	e.probeCyphers = append(e.probeCyphers, cypher)
	if e.probeErr != nil {
		return false, e.probeErr
	}
	return e.probePresent, nil
}

func deploymentSourceOnlyProjection() *ProjectionResult {
	return &ProjectionResult{
		DeploymentSourceRows: []DeploymentSourceRow{
			{
				DeploymentRepoID: "deploy-repo-1",
				Environment:      "production",
				InstanceID:       "workload-instance:my-api:production",
				WorkloadName:     "my-api",
				Confidence:       0.96,
				Provenance:       []string{"argocd_application_source"},
			},
		},
	}
}

func executedDeploymentSource(executed []string) bool {
	for _, cypher := range executed {
		if strings.Contains(cypher, "DEPLOYMENT_SOURCE") {
			return true
		}
	}
	return false
}

// TestWorkloadMaterializerDeploymentSourceAbsentTargetDefers is the #6184
// shard-3 regression: a deployment-source batch whose Repository or instance
// target is absent from the graph must fail retryably instead of completing
// with a silent zero-edge write that still counts DeploymentSourcesWritten.
// The deployment node is committed by another scope's materialization with no
// happens-before against this batch, so a drain that reaches the write first
// MATCHes nothing, the worker completes the intent, and the edge is lost
// with no error and no dead letter.
func TestWorkloadMaterializerDeploymentSourceAbsentTargetDefers(t *testing.T) {
	t.Parallel()

	executor := &probeScriptMaterializerExecutor{probePresent: false}
	m := NewWorkloadMaterializer(executor)
	m.DeploymentSourceProber = executor

	result, err := m.Materialize(context.Background(), deploymentSourceOnlyProjection())
	if err == nil {
		t.Fatal("Materialize() with an absent deployment target succeeded silently, want a retryable error so the pass re-runs after the target commits")
	}
	if !IsRetryable(err) {
		t.Fatalf("Materialize() error = %v, want a retryable error so the intent stays queued", err)
	}
	if result.DeploymentSourcesWritten != 0 {
		t.Fatalf("DeploymentSourcesWritten = %d, want 0: a deferred batch must not count rows as written", result.DeploymentSourcesWritten)
	}
	if executedDeploymentSource(executor.executedCyphers) {
		t.Fatal("deployment-source statement ran despite an absent target: no edge statement may run for a deferred batch")
	}
	if executor.probeCalls == 0 {
		t.Fatal("target existence probe was never consulted")
	}
}

// TestWorkloadMaterializerDeploymentSourceProbeNamesBothEndpoints pins the
// probe shape the guard relies on: one anchored MATCH per distinct instance
// and deployment repo in the batch, read-only.
func TestWorkloadMaterializerDeploymentSourceProbeNamesBothEndpoints(t *testing.T) {
	t.Parallel()

	executor := &probeScriptMaterializerExecutor{probePresent: true}
	m := NewWorkloadMaterializer(executor)
	m.DeploymentSourceProber = executor

	if _, err := m.Materialize(context.Background(), deploymentSourceOnlyProjection()); err != nil {
		t.Fatalf("Materialize() with all targets present error = %v", err)
	}
	if executor.probeCalls == 0 {
		t.Fatal("target existence probe was never consulted")
	}
	probe := executor.probeCyphers[0]
	for _, want := range []string{"WorkloadInstance", ":Repository", "RETURN 1 LIMIT 1"} {
		if !strings.Contains(probe, want) {
			t.Fatalf("probe cypher missing %q:\n%s", want, probe)
		}
	}
	for _, forbidden := range []string{"MERGE", "CREATE", "DELETE", "SET ", "OPTIONAL", "WITH", "WHERE", "COUNT", "UNWIND"} {
		if strings.Contains(probe, forbidden) {
			t.Fatalf("probe cypher must be anchored MATCHes only, found %q:\n%s", forbidden, probe)
		}
	}
	if !executedDeploymentSource(executor.executedCyphers) {
		t.Fatal("expected the deployment-source write to run when all targets are present")
	}
}

// TestWorkloadMaterializerDeploymentSourceProbeErrorFallsBackToWrite pins
// the infrastructure failure direction: a failing existence probe must not
// stall the partition on work that may be perfectly writable.
func TestWorkloadMaterializerDeploymentSourceProbeErrorFallsBackToWrite(t *testing.T) {
	t.Parallel()

	executor := &probeScriptMaterializerExecutor{probeErr: errors.New("probe backend unavailable")}
	m := NewWorkloadMaterializer(executor)
	m.DeploymentSourceProber = executor

	result, err := m.Materialize(context.Background(), deploymentSourceOnlyProjection())
	if err != nil {
		t.Fatalf("Materialize() with a failing probe error = %v, want the legacy fail-open write", err)
	}
	if result.DeploymentSourcesWritten != 1 {
		t.Fatalf("DeploymentSourcesWritten = %d, want 1 on the fail-open path", result.DeploymentSourcesWritten)
	}
	if !executedDeploymentSource(executor.executedCyphers) {
		t.Fatal("expected the deployment-source write to run when the probe itself fails")
	}
}

// TestWorkloadMaterializerDeploymentSourceMissCountsTargetMiss is the
// telemetry half of the #6184 shard-3 regression: a deferred
// deployment-source batch must increment eshu_dp_shared_edge_target_miss_total
// with the workload_materialization domain, the same operator signal the
// shared-edge guard emits, so a stall is distinguishable from a healthy idle
// drain at 3 AM.
func TestWorkloadMaterializerDeploymentSourceMissCountsTargetMiss(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	executor := &probeScriptMaterializerExecutor{probePresent: false}
	m := NewWorkloadMaterializer(executor)
	m.DeploymentSourceProber = executor
	m.Instruments = inst

	if _, err := m.Materialize(context.Background(), deploymentSourceOnlyProjection()); err == nil {
		t.Fatal("Materialize() with an absent deployment target succeeded, want the retryable deferral")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	attrs := map[string]string{
		telemetry.MetricDimensionDomain: string(DomainWorkloadMaterialization),
	}
	if got := reducerCounterValue(t, rm, "eshu_dp_shared_edge_target_miss_total", attrs); got != 1 {
		t.Fatalf("shared_edge_target_miss_total[workload_materialization] = %d, want 1", got)
	}
}
