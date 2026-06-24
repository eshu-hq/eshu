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

// ec2UsesProfileReadinessQueueDB is a fake reducer-queue backend that returns one
// pending ec2_uses_profile_materialization intent only when BOTH endpoint node
// phases are committed. It proves the durable SQL gate (not just the in-handler
// ReadinessLookup) holds USES_PROFILE edge work until BOTH the EC2 instance node
// phase (ec2_instance_node_materialization:<scope>) AND the IAM instance-profile
// node phase (aws_resource_materialization:<scope>) commit, so a USES_PROFILE edge
// never resolves against an endpoint that has not materialized.
//
// The two endpoint phases publish under DIFFERENT entity keys, so the gate cannot
// reuse the single payload->>'entity_key' match the single-phase edges use; it
// must require both literal-prefix entity keys. The fake models that by tracking
// each phase independently and returning the waiting intent only when both are set.
type ec2UsesProfileReadinessQueueDB struct {
	now               time.Time
	instanceNodeReady bool
	profileNodeReady  bool
	status            string
	attemptCount      int
	claimQueries      int
}

func (db *ec2UsesProfileReadinessQueueDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeResult{}, nil
}

func (db *ec2UsesProfileReadinessQueueDB) QueryContext(_ context.Context, query string, _ ...any) (Rows, error) {
	if !strings.Contains(query, "FROM fact_work_items") || !strings.Contains(query, "FROM claimed") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	db.claimQueries++

	if !strings.Contains(query, "ec2_uses_profile_materialization") {
		return nil, fmt.Errorf("claim query missing ec2 uses-profile readiness gate:\n%s", query)
	}

	// The dual-key gate must require BOTH the instance node phase (under the
	// ec2_instance_node_materialization entity key) AND the instance-profile node
	// phase (under the aws_resource_materialization entity key), both on the
	// cloud_resource_uid keyspace's canonical_nodes_committed phase.
	gatesInstanceNode := queryHasScopePrefixReadinessRequirement(
		query,
		string(reducer.DomainEC2UsesProfileMaterialization),
		"cloud_resource_uid",
		"canonical_nodes_committed",
		"ec2_instance_node_materialization:",
	)
	gatesProfileNode := queryHasScopePrefixReadinessRequirement(
		query,
		string(reducer.DomainEC2UsesProfileMaterialization),
		"cloud_resource_uid",
		"canonical_nodes_committed",
		"aws_resource_materialization:",
	)
	if !gatesInstanceNode {
		return nil, fmt.Errorf("claim query missing the EC2 instance node phase requirement:\n%s", query)
	}
	if !gatesProfileNode {
		return nil, fmt.Errorf("claim query missing the IAM instance-profile node phase requirement:\n%s", query)
	}

	// Model the dual-key gate: the intent is only returned when BOTH phases are
	// present. A single committed phase keeps the work waiting.
	if !db.instanceNodeReady || !db.profileNodeReady {
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
		"aws:111122223333:us-east-1:ec2",
		"gen-aws-1",
		string(reducer.DomainEC2UsesProfileMaterialization),
		db.attemptCount + 1,
		db.now.Add(-time.Minute),
		db.now.Add(-time.Minute),
		[]byte(`{"entity_key":"ec2_uses_profile_materialization:aws:111122223333:us-east-1:ec2","reason":"ec2 instance profile usage observed","fact_id":"fact-profile-1","source_system":"aws"}`),
	}}}, nil
}

func ec2UsesProfileReadinessQueue(db *ec2UsesProfileReadinessQueueDB, now time.Time) ReducerQueue {
	return ReducerQueue{
		db:            db,
		LeaseOwner:    "test-owner",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
}

func TestReducerQueueClaimWaitsForEC2UsesProfileDualReadinessBehavior(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 31, 11, 10, 0, 0, time.UTC)

	cases := []struct {
		name              string
		instanceNodeReady bool
		profileNodeReady  bool
		wantClaimed       bool
	}{
		{name: "neither phase committed", instanceNodeReady: false, profileNodeReady: false, wantClaimed: false},
		{name: "only instance node phase committed", instanceNodeReady: true, profileNodeReady: false, wantClaimed: false},
		{name: "only profile node phase committed", instanceNodeReady: false, profileNodeReady: true, wantClaimed: false},
		{name: "both phases committed", instanceNodeReady: true, profileNodeReady: true, wantClaimed: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := &ec2UsesProfileReadinessQueueDB{
				now:               now,
				instanceNodeReady: tc.instanceNodeReady,
				profileNodeReady:  tc.profileNodeReady,
				status:            "pending",
			}
			queue := ec2UsesProfileReadinessQueue(db, now)

			intent, claimed, err := queue.Claim(context.Background())
			if err != nil {
				t.Fatalf("Claim() error = %v", err)
			}
			if claimed != tc.wantClaimed {
				t.Fatalf("Claim() claimed = %v, want %v (instanceNodeReady=%v profileNodeReady=%v)",
					claimed, tc.wantClaimed, tc.instanceNodeReady, tc.profileNodeReady)
			}
			if !tc.wantClaimed {
				return
			}
			if got, want := intent.Domain, reducer.DomainEC2UsesProfileMaterialization; got != want {
				t.Fatalf("claimed domain = %q, want %q", got, want)
			}
			if got, want := intent.EntityKeys, []string{"ec2_uses_profile_materialization:aws:111122223333:us-east-1:ec2"}; fmt.Sprint(got) != fmt.Sprint(want) {
				t.Fatalf("claimed entity keys = %v, want %v", got, want)
			}
		})
	}
}
