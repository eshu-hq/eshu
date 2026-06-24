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

func TestReducerQueueClaimGatesIAMInstanceProfileRoleOnCloudResourceNodes(t *testing.T) {
	t.Parallel()

	for _, query := range []string{claimReducerWorkQuery, claimReducerWorkBatchQuery} {
		if !strings.Contains(query, "iam_instance_profile_role_materialization") {
			t.Fatalf("claim query missing iam_instance_profile_role_materialization readiness gate:\n%s", query)
		}
		if !strings.Contains(query, "aws_resource_materialization") {
			t.Fatalf("claim query must gate profile-role edges on aws_resource_materialization entity keys:\n%s", query)
		}
		if !strings.Contains(query, "cloud_resource_uid") || !strings.Contains(query, "canonical_nodes_committed") {
			t.Fatalf("claim query must require the CloudResource canonical-nodes phase:\n%s", query)
		}
	}
}

type iamInstanceProfileRoleReadinessQueueDB struct {
	now        time.Time
	phaseReady bool
}

func (db *iamInstanceProfileRoleReadinessQueueDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeResult{}, nil
}

func (db *iamInstanceProfileRoleReadinessQueueDB) QueryContext(_ context.Context, query string, _ ...any) (Rows, error) {
	if !strings.Contains(query, "FROM fact_work_items") || !strings.Contains(query, "FROM claimed") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	if !strings.Contains(query, "iam_instance_profile_role_materialization") {
		return nil, fmt.Errorf("claim query missing iam instance-profile role readiness gate:\n%s", query)
	}
	hasReadinessGate := queryHasBoundedReadinessRequirement(
		query,
		string(reducer.DomainIAMInstanceProfileRoleMaterialization),
		"cloud_resource_uid",
		"canonical_nodes_committed",
	) && queryHasPayloadReadinessLookup(query, "fact_work_items", "readiness_req", "readiness_phase")
	if hasReadinessGate && !db.phaseReady {
		return &queueFakeRows{}, nil
	}
	return &queueFakeRows{rows: [][]any{{
		"reducer-profile-role-1",
		"aws:123456789012:aws-global:iam",
		"gen-aws-1",
		string(reducer.DomainIAMInstanceProfileRoleMaterialization),
		1,
		db.now.Add(-time.Minute),
		db.now.Add(-time.Minute),
		[]byte(`{"entity_key":"aws_resource_materialization:aws:123456789012:aws-global:iam","reason":"iam instance profile roles observed","fact_id":"fact-profile-1","source_system":"aws"}`),
	}}}, nil
}

func TestReducerQueueClaimWaitsForIAMInstanceProfileRoleReadinessBehavior(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 2, 10, 0, 0, 0, time.UTC)
	db := &iamInstanceProfileRoleReadinessQueueDB{now: now}
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
	if claimed {
		t.Fatalf("Claim() claimed %q before canonical readiness, want pending", intent.IntentID)
	}

	db.phaseReady = true
	intent, claimed, err = queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() after readiness error = %v", err)
	}
	if !claimed {
		t.Fatal("Claim() after readiness claimed = false, want true")
	}
	if got, want := intent.Domain, reducer.DomainIAMInstanceProfileRoleMaterialization; got != want {
		t.Fatalf("claimed domain = %q, want %q", got, want)
	}
	if got, want := intent.EntityKeys, []string{"aws_resource_materialization:aws:123456789012:aws-global:iam"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("claimed entity keys = %v, want %v", got, want)
	}
}
