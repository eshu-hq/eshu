// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graphbackpressure"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestProductionWorkloadMaterializerWiresDeploymentSourceProber pins the
// production chain buildReducerService assembles for the workload materializer
// (newReducerCypherExecutor -> boundCypherExecutor -> newProbedWorkloadMaterializer)
// so a refactor of an adapter cannot silently return the deployment-source
// guard to the legacy unconditional write (#6759). It runs with the graph
// write gate disabled (passthrough) and enabled (cypherExecutorGate wrapper),
// and drives the guard through Materialize against a session whose probe
// reports the deploy Repository absent, then present.
func TestProductionWorkloadMaterializerWiresDeploymentSourceProber(t *testing.T) {
	t.Parallel()

	for name, env := range map[string]map[string]string{
		"gate disabled": {},
		"gate enabled":  {graphbackpressure.MaxInFlightEnv: "4"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			getenv := func(key string) string { return env[key] }
			build := func(probeFound bool) (*reducer.WorkloadMaterializer, *fakeNeo4jSession) {
				session := &fakeNeo4jSession{probeFound: probeFound}
				cypherExec := newReducerGraphWriteGate(getenv, nil).boundCypherExecutor(
					newReducerCypherExecutor(session, nil, nil),
				)
				return newProbedWorkloadMaterializer(cypherExec, nil, nil), session
			}
			projection := &reducer.ProjectionResult{
				DeploymentSourceRows: []reducer.DeploymentSourceRow{{
					DeploymentRepoID: "deploy-repo-1",
					Environment:      "production",
					InstanceID:       "workload-instance:my-api:production",
					WorkloadName:     "my-api",
					Confidence:       0.96,
					Provenance:       []string{"argocd_application_source"},
				}},
			}

			materializer, absentSession := build(false)
			if materializer.DeploymentSourceProber == nil {
				t.Fatal("production chain left DeploymentSourceProber nil: the deployment-source guard is inert")
			}
			if _, err := materializer.Materialize(context.Background(), projection); err == nil || !reducer.IsRetryable(err) {
				t.Fatalf("Materialize() with an absent target error = %v, want a retryable deferral", err)
			}
			if len(absentSession.probeCalls) == 0 {
				t.Fatal("the session probe was never consulted through the production chain")
			}
			for _, call := range absentSession.calls {
				if strings.Contains(call.Cypher, "DEPLOYMENT_SOURCE") {
					t.Fatalf("deployment-source write ran despite an absent target:\n%s", call.Cypher)
				}
			}

			materializer, presentSession := build(true)
			if _, err := materializer.Materialize(context.Background(), projection); err != nil {
				t.Fatalf("Materialize() with a present target error = %v, want nil", err)
			}
			wrote := false
			for _, call := range presentSession.calls {
				wrote = wrote || strings.Contains(call.Cypher, "DEPLOYMENT_SOURCE")
			}
			if !wrote {
				t.Fatal("deployment-source write never reached the session with all targets present")
			}
		})
	}
}
