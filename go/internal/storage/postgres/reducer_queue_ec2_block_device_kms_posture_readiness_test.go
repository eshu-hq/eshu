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

type ec2BlockDeviceKMSPostureReadinessQueueDB struct {
	now               time.Time
	instanceNodeReady bool
	resourceNodeReady bool
}

func (db *ec2BlockDeviceKMSPostureReadinessQueueDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeResult{}, nil
}

func (db *ec2BlockDeviceKMSPostureReadinessQueueDB) QueryContext(_ context.Context, query string, _ ...any) (Rows, error) {
	if !strings.Contains(query, "FROM fact_work_items") || !strings.Contains(query, "FROM claimed") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	if !strings.Contains(query, "ec2_block_device_kms_posture_materialization") {
		return nil, fmt.Errorf("claim query missing ec2 block-device KMS posture readiness gate:\n%s", query)
	}

	gatesInstanceNode := queryHasScopePrefixReadinessRequirement(
		query,
		string(reducer.DomainEC2BlockDeviceKMSPostureMaterialization),
		"cloud_resource_uid",
		"canonical_nodes_committed",
		"ec2_instance_node_materialization:",
	)
	gatesResourceNode := queryHasScopePrefixReadinessRequirement(
		query,
		string(reducer.DomainEC2BlockDeviceKMSPostureMaterialization),
		"cloud_resource_uid",
		"canonical_nodes_committed",
		"aws_resource_materialization:",
	)
	if !gatesInstanceNode {
		return nil, fmt.Errorf("claim query missing the EC2 instance node phase requirement:\n%s", query)
	}
	if !gatesResourceNode {
		return nil, fmt.Errorf("claim query missing the EBS/KMS resource node phase requirement:\n%s", query)
	}
	if !db.instanceNodeReady || !db.resourceNodeReady {
		return &queueFakeRows{}, nil
	}

	return &queueFakeRows{rows: [][]any{{
		"reducer-ec2-block-device-kms-1",
		"aws:111122223333:us-east-1:ec2",
		"gen-aws-1",
		string(reducer.DomainEC2BlockDeviceKMSPostureMaterialization),
		1,
		db.now.Add(-time.Minute),
		db.now.Add(-time.Minute),
		[]byte(`{"entity_key":"ec2_block_device_kms_posture_materialization:aws:111122223333:us-east-1:ec2","reason":"ec2 block-device posture observed","fact_id":"fact-posture-1","source_system":"aws"}`),
	}}}, nil
}

func TestReducerQueueClaimWaitsForEC2BlockDeviceKMSPostureDualReadinessBehavior(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 2, 11, 10, 0, 0, time.UTC)
	cases := []struct {
		name              string
		instanceNodeReady bool
		resourceNodeReady bool
		wantClaimed       bool
	}{
		{name: "neither phase committed", instanceNodeReady: false, resourceNodeReady: false, wantClaimed: false},
		{name: "only instance node phase committed", instanceNodeReady: true, resourceNodeReady: false, wantClaimed: false},
		{name: "only resource node phase committed", instanceNodeReady: false, resourceNodeReady: true, wantClaimed: false},
		{name: "both phases committed", instanceNodeReady: true, resourceNodeReady: true, wantClaimed: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := &ec2BlockDeviceKMSPostureReadinessQueueDB{
				now:               now,
				instanceNodeReady: tc.instanceNodeReady,
				resourceNodeReady: tc.resourceNodeReady,
			}
			queue := ReducerQueue{
				db:            db,
				LeaseOwner:    "test-owner",
				LeaseDuration: time.Minute,
				Now:           func() time.Time { return now },
			}

			intent, claimed, err := queue.Claim(context.Background())
			if err != nil {
				t.Fatalf("Claim() error = %v", err)
			}
			if claimed != tc.wantClaimed {
				t.Fatalf("Claim() claimed = %v, want %v", claimed, tc.wantClaimed)
			}
			if !tc.wantClaimed {
				return
			}
			if got, want := intent.Domain, reducer.DomainEC2BlockDeviceKMSPostureMaterialization; got != want {
				t.Fatalf("claimed domain = %q, want %q", got, want)
			}
		})
	}
}

func TestReducerConflictBlockageReportsEC2BlockDeviceKMSPostureReadiness(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"ec2_block_device_kms_posture_materialization",
		"ec2_instance_node_materialization:",
		"aws_resource_materialization:",
	} {
		if !strings.Contains(reducerConflictBlockageQuery, want) {
			t.Fatalf("blockage query missing EC2 block-device KMS posture readiness token %q:\n%s", want, reducerConflictBlockageQuery)
		}
	}
}
