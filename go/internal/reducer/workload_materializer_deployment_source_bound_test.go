// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// handleWithAbsentDeploymentSource runs the real handler over a workload whose
// deploy Repository never appears in the graph, so the deployment-source guard
// defers every pass. anchor sets the intent's repair-cycle timestamps.
func handleWithAbsentDeploymentSource(t *testing.T, cycleStartedAt, enqueuedAt time.Time) error {
	t.Helper()

	now := time.Now().UTC()
	loader := &stubFactLoader{envelopes: []facts.Envelope{
		{
			FactID:     "fact-repo",
			FactKind:   "repository",
			Payload:    map[string]any{"graph_id": "repo-service-edge-api", "name": "service-edge-api"},
			ObservedAt: now,
		},
		{
			FactID:   "fact-file",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id": "repo-service-edge-api",
				"parsed_file_data": map[string]any{
					"k8s_resources": []any{map[string]any{
						"name": "service-edge-api", "kind": "Deployment", "namespace": "production",
					}},
				},
			},
			ObservedAt: now,
		},
	}}
	relationshipLoader := &stubResolvedRelationshipLoader{resolved: []relationships.ResolvedRelationship{{
		SourceRepoID:     "repo-service-edge-api",
		TargetRepoID:     "repo-deployment-kustomize",
		RelationshipType: relationships.RelDeploysFrom,
		Details: map[string]any{
			"evidence_kinds": []any{string(relationships.EvidenceKindArgoCDApplicationSetDeploySource)},
		},
	}}}

	executor := &probeScriptMaterializerExecutor{probePresent: false}
	materializer := NewWorkloadMaterializer(executor)
	materializer.DeploymentSourceProber = executor
	handler := WorkloadMaterializationHandler{
		FactLoader:     loader,
		ResolvedLoader: relationshipLoader,
		Materializer:   materializer,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:        "intent-wm-deploy-source-bound",
		ScopeID:         "scope-service-edge-api",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "facts projected",
		EntityKeys:      []string{"repo-service-edge-api"},
		RelatedScopeIDs: []string{"scope-service-edge-api"},
		EnqueuedAt:      enqueuedAt,
		CycleStartedAt:  cycleStartedAt,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	})
	if err == nil {
		t.Fatal("Handle() with an absent deployment target succeeded, want a retryable deferral")
	}
	if !IsRetryable(err) {
		t.Fatalf("Handle() error = %v, want a retryable error", err)
	}
	if executedDeploymentSource(executor.executedCyphers) {
		t.Fatal("deployment-source statement ran despite an absent target")
	}
	return err
}

func failureClassOf(err error) (string, bool) {
	var classed interface{ FailureClass() string }
	if errors.As(err, &classed) {
		return classed.FailureClass(), true
	}
	return "", false
}

// TestWorkloadMaterializationDeploymentSourceDeferralIsBoundedByElapsedTime is
// the #6759 liveness proof. The target-not-ready deferral is non-counting, so
// attempt_count freezes and cannot bound it; an unbounded wait would leave a
// deploy Repository that never appears retrying forever and the generation
// never completing. Past crossscope.ProducerReadinessMaxWait since the repair
// cycle began, the pass must fail with a COUNTING retryable error so the
// ordinary budget dead-letters it loudly. Inside the bound it stays the
// non-counting deferral.
func TestWorkloadMaterializationDeploymentSourceDeferralIsBoundedByElapsedTime(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	pastBound := now.Add(-crossscope.ProducerReadinessMaxWait - time.Minute)

	t.Run("inside the bound stays non-counting", func(t *testing.T) {
		t.Parallel()
		err := handleWithAbsentDeploymentSource(t, now.Add(-time.Minute), now.Add(-time.Hour))
		class, ok := failureClassOf(err)
		if !ok || class != WorkloadMaterializationDeploymentSourceTargetNotReadyFailureClass {
			t.Fatalf("failure class = %q (found=%v), want %q", class, ok, WorkloadMaterializationDeploymentSourceTargetNotReadyFailureClass)
		}
	})

	t.Run("past the bound counts toward the retry budget", func(t *testing.T) {
		t.Parallel()
		err := handleWithAbsentDeploymentSource(t, pastBound, pastBound)
		if class, ok := failureClassOf(err); ok {
			t.Fatalf("failure class = %q past the bound, want none so the retry budget counts and dead-letters it", class)
		}
		for _, want := range []string{"deployment-source target", "elapsed", crossscope.ProducerReadinessMaxWait.String()} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error %q missing %q: the operator must see the elapsed wait and the bound", err, want)
			}
		}
	})

	t.Run("cycle start anchors the bound, not first enqueue", func(t *testing.T) {
		t.Parallel()
		// A reopened row: created long ago, reopened a minute ago. Anchoring on
		// EnqueuedAt would dead-letter it on its first claim of the new cycle.
		err := handleWithAbsentDeploymentSource(t, now.Add(-time.Minute), pastBound)
		if class, ok := failureClassOf(err); !ok || class != WorkloadMaterializationDeploymentSourceTargetNotReadyFailureClass {
			t.Fatalf("failure class = %q (found=%v), want the non-counting class for a freshly reopened row", class, ok)
		}
	})

	t.Run("unknown elapsed time keeps deferring", func(t *testing.T) {
		t.Parallel()
		err := handleWithAbsentDeploymentSource(t, time.Time{}, time.Time{})
		if class, ok := failureClassOf(err); !ok || class != WorkloadMaterializationDeploymentSourceTargetNotReadyFailureClass {
			t.Fatalf("failure class = %q (found=%v), want the non-counting class when the anchor is unknown", class, ok)
		}
	})
}
