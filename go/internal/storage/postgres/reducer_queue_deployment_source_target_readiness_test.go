// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// absentTargetProber reports every graph existence probe as a miss, the state
// a deployment Repository node is in until another scope's repo_dependency
// write commits it.
type absentTargetProber struct{}

func (absentTargetProber) ProbeGraphExists(context.Context, string, map[string]any) (bool, error) {
	return false, nil
}

// discardCypherExecutor accepts every write; the guard must fail the pass
// before any deployment-source statement runs.
type discardCypherExecutor struct{}

func (discardCypherExecutor) ExecuteCypher(context.Context, string, map[string]any) error {
	return nil
}

// TestReducerQueueFailDefersDeploymentSourceTargetMissPastAttemptBudget is the
// #6759 regression. The deployment-source guard (#6730) fails the pass with a
// retryable error when the deploy Repository node is not yet committed, and
// documents that the miss "must never be terminalized". Without a
// non-counting failure class the durable queue counted every such retry toward
// MaxAttempts, so a slow repo_dependency lane dead-lettered the
// workload_materialization intent and DEPLOYMENT_SOURCE was lost, loudly this
// time. The error is produced by the real guard through Materialize, not a
// stand-in, so the test also proves the class travels on the real error.
func TestReducerQueueFailDefersDeploymentSourceTargetMissPastAttemptBudget(t *testing.T) {
	t.Parallel()

	materializer := reducer.NewWorkloadMaterializer(discardCypherExecutor{})
	materializer.DeploymentSourceProber = absentTargetProber{}
	_, cause := materializer.Materialize(context.Background(), &reducer.ProjectionResult{
		DeploymentSourceRows: []reducer.DeploymentSourceRow{{
			DeploymentRepoID: "deploy-repo-1",
			Environment:      "production",
			InstanceID:       "workload-instance:my-api:production",
			WorkloadName:     "my-api",
			Confidence:       0.96,
			Provenance:       []string{"argocd_application_source"},
		}},
	})
	if cause == nil {
		t.Fatal("Materialize() with an absent deployment target returned nil, want the guard's deferral error")
	}

	now := time.Date(2026, time.September, 25, 11, 0, 0, 0, time.UTC)
	database := &fakeExecQueryer{}
	queue := ReducerQueue{
		database:      database,
		LeaseOwner:    "reducer-1",
		LeaseDuration: time.Minute,
		RetryDelay:    2 * time.Minute,
		MaxAttempts:   3,
		Now:           func() time.Time { return now },
	}
	intent := reducer.Intent{
		IntentID:     "intent-workload-materialization-1",
		AttemptCount: 42,
	}

	if err := queue.Fail(context.Background(), intent, cause); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}
	if got, want := len(database.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}

	query := database.execs[0].query
	for _, want := range []string{
		"UPDATE fact_work_items",
		"status = 'retrying'",
		"next_attempt_at = $5",
		"failure_class = $2",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("deferred retry query missing %q (the intent was dead-lettered past the retry budget):\n%s", want, query)
		}
	}
	if got, want := database.execs[0].args[1], reducer.WorkloadMaterializationDeploymentSourceTargetNotReadyFailureClass; got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
}
