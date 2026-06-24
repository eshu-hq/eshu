// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// kubernetesCorrelationReadinessQueueDB is a fake reducer-queue backend that
// returns one pending kubernetes_correlation_materialization intent only when the
// KubernetesWorkload canonical-nodes readiness gate is satisfied. It proves the
// durable SQL gate (not just the in-handler ReadinessLookup) holds RUNS_IMAGE edge
// work until the #388 node slice's KubernetesWorkload nodes commit on the
// kubernetes_workload_uid keyspace — a different keyspace than the AWS/COVERS
// cloud_resource_uid gate, so it requires its own clause.
type kubernetesCorrelationReadinessQueueDB struct {
	now          time.Time
	phaseReady   bool
	status       string
	attemptCount int
	claimQueries int
}

func (db *kubernetesCorrelationReadinessQueueDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeResult{}, nil
}

func (db *kubernetesCorrelationReadinessQueueDB) QueryContext(_ context.Context, query string, _ ...any) (Rows, error) {
	if !strings.Contains(query, "FROM fact_work_items") || !strings.Contains(query, "FROM claimed") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	db.claimQueries++

	// The claim gate must fence the kubernetes edge domain on the workload-uid
	// keyspace, not the cloud_resource_uid keyspace the AWS/COVERS edges use.
	if !strings.Contains(query, "kubernetes_correlation_materialization") {
		return nil, fmt.Errorf("claim query missing kubernetes correlation readiness gate:\n%s", query)
	}
	if !strings.Contains(query, "kubernetes_workload_uid") {
		return nil, fmt.Errorf("claim query missing kubernetes_workload_uid keyspace gate:\n%s", query)
	}

	hasReadinessGate := queryHasBoundedReadinessRequirement(
		query,
		string(reducer.DomainKubernetesCorrelationMaterialization),
		"kubernetes_workload_uid",
		"canonical_nodes_committed",
	) && queryHasPayloadReadinessLookup(query, "fact_work_items", "readiness_req", "readiness_phase")
	if hasReadinessGate && !db.phaseReady {
		return &queueFakeRows{}, nil
	}

	status := strings.TrimSpace(db.status)
	if status == "" {
		status = "pending"
	}
	if status != "pending" && status != "retrying" {
		return &queueFakeRows{}, nil
	}

	return &queueFakeRows{rows: [][]any{{
		"reducer-k8s-edge-1",
		"k8s:prod-us-east-1",
		"gen-k8s-1",
		string(reducer.DomainKubernetesCorrelationMaterialization),
		db.attemptCount + 1,
		db.now.Add(-time.Minute),
		db.now.Add(-time.Minute),
		[]byte(`{"entity_key":"kubernetes_workload_materialization:k8s:prod-us-east-1","reason":"kubernetes live workload facts observed","fact_id":"fact-pod-1","source_system":"kubernetes"}`),
	}}}, nil
}

func kubernetesCorrelationReadinessQueue(db *kubernetesCorrelationReadinessQueueDB, now time.Time) ReducerQueue {
	return ReducerQueue{
		db:            db,
		LeaseOwner:    "test-owner",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
}

func TestReducerQueueClaimWaitsForKubernetesWorkloadReadinessBehavior(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 1, 11, 10, 0, 0, time.UTC)
	db := &kubernetesCorrelationReadinessQueueDB{
		now:        now,
		phaseReady: false,
		status:     "pending",
	}
	queue := kubernetesCorrelationReadinessQueue(db, now)

	intent, claimed, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if claimed {
		t.Fatalf("Claim() claimed %q before workload-node readiness, want unclaimed waiting work", intent.IntentID)
	}

	db.phaseReady = true
	intent, claimed, err = queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() after readiness error = %v", err)
	}
	if !claimed {
		t.Fatal("Claim() after readiness claimed = false, want true")
	}
	if got, want := intent.Domain, reducer.DomainKubernetesCorrelationMaterialization; got != want {
		t.Fatalf("claimed domain = %q, want %q", got, want)
	}
	if got, want := intent.EntityKeys, []string{"kubernetes_workload_materialization:k8s:prod-us-east-1"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("claimed entity keys = %v, want %v", got, want)
	}
}
