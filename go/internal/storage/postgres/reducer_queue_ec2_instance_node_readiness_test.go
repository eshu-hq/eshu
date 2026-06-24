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

// ec2InstanceNodeReadinessQueueDB is a fake reducer-queue backend that proves the
// EC2 instance node domain's published cloud_resource_uid / canonical_nodes_committed
// phase feeds the durable Postgres readiness gate. PR-A (#1146) is a node
// publisher, not an edge consumer, so it adds no new gate clause; the future
// USES_PROFILE edge (PR-B) will gate on cloud_resource_uid for the EC2 node's
// distinct entity key (ec2_instance_node_materialization:<scope>). This fake drives
// the existing cloud_resource_uid EXISTS guard with that EC2 node entity key in the
// claim payload, proving the gate's acceptance_unit_id = entity_key join resolves
// the phase row the EC2 node domain publishes — so once instance nodes commit, an
// edge keyed on the EC2 node entity key becomes claimable.
type ec2InstanceNodeReadinessQueueDB struct {
	now          time.Time
	phaseReady   bool
	status       string
	attemptCount int
	claimQueries int
}

func (db *ec2InstanceNodeReadinessQueueDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeResult{}, nil
}

func (db *ec2InstanceNodeReadinessQueueDB) QueryContext(_ context.Context, query string, _ ...any) (Rows, error) {
	if !strings.Contains(query, "FROM fact_work_items") || !strings.Contains(query, "FROM claimed") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	db.claimQueries++

	// The durable gate must resolve the cloud_resource_uid canonical-nodes phase by
	// the work item's own entity_key, so the EC2 node domain's distinct entity key
	// (ec2_instance_node_materialization:<scope>) is the acceptance unit the future
	// USES_PROFILE edge will gate on.
	hasCloudResourceGate := queryHasBoundedReadinessRequirement(
		query,
		string(reducer.DomainAWSRelationshipMaterialization),
		"cloud_resource_uid",
		"canonical_nodes_committed",
	) && queryHasPayloadReadinessLookup(query, "fact_work_items", "readiness_req", "readiness_phase")
	if !hasCloudResourceGate {
		return nil, fmt.Errorf("claim query missing entity-key-joined cloud_resource_uid readiness gate:\n%s", query)
	}
	if !db.phaseReady {
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
		"reducer-ec2-uses-profile-1",
		"aws:123456789012:us-east-1:ec2",
		"gen-ec2-1",
		// Drive an existing gated edge domain (aws_relationship_materialization) with
		// the EC2 node entity key, proving the cloud_resource_uid EXISTS guard
		// resolves the phase the EC2 node domain publishes under that key. PR-B swaps
		// this domain for the real ec2_uses_profile_materialization clause.
		string(reducer.DomainAWSRelationshipMaterialization),
		db.attemptCount + 1,
		db.now.Add(-time.Minute),
		db.now.Add(-time.Minute),
		[]byte(`{"entity_key":"ec2_instance_node_materialization:aws:123456789012:us-east-1:ec2","reason":"ec2 instance posture facts observed","fact_id":"fact-ec2-1","source_system":"aws"}`),
	}}}, nil
}

func ec2InstanceNodeReadinessQueue(db *ec2InstanceNodeReadinessQueueDB, now time.Time) ReducerQueue {
	return ReducerQueue{
		db:            db,
		LeaseOwner:    "test-owner",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
}

// TestReducerQueueClaimWaitsForEC2InstanceNodeReadinessBehavior proves the durable
// claim gate holds a cloud_resource_uid-gated edge keyed on the EC2 node entity key
// until the EC2 instance node domain's canonical-nodes phase row exists, then
// releases it. This is the readiness proof for PR-A: the new node source feeds the
// existing durable gate without inventing a not-yet-built edge domain.
func TestReducerQueueClaimWaitsForEC2InstanceNodeReadinessBehavior(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 1, 12, 10, 0, 0, time.UTC)
	db := &ec2InstanceNodeReadinessQueueDB{
		now:        now,
		phaseReady: false,
		status:     "pending",
	}
	queue := ec2InstanceNodeReadinessQueue(db, now)

	intent, claimed, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if claimed {
		t.Fatalf("Claim() claimed %q before EC2 instance nodes committed, want unclaimed waiting work", intent.IntentID)
	}
	if db.claimQueries != 1 {
		t.Fatalf("claim queries = %d, want 1", db.claimQueries)
	}

	db.phaseReady = true
	intent, claimed, err = queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() after readiness error = %v", err)
	}
	if !claimed {
		t.Fatal("Claim() after EC2 instance nodes committed claimed = false, want claimable")
	}
	if got, want := intent.EntityKeys, []string{"ec2_instance_node_materialization:aws:123456789012:us-east-1:ec2"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("claimed entity keys = %v, want the EC2 node entity key %v", got, want)
	}
}

func TestReducerQueueClaimEC2InstanceNodeAlreadyReadyBehavior(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 1, 12, 25, 0, 0, time.UTC)
	db := &ec2InstanceNodeReadinessQueueDB{
		now:        now,
		phaseReady: true,
		status:     "pending",
	}
	queue := ec2InstanceNodeReadinessQueue(db, now)

	intent, claimed, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if !claimed {
		t.Fatal("Claim() claimed = false, want already-committed EC2 node phase to release the gated work")
	}
	if got, want := intent.AttemptCount, 1; got != want {
		t.Fatalf("claimed attempt count = %d, want first claim attempt %d", got, want)
	}
}
