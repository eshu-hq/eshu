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

// rdsPostureReadinessQueueDB proves the durable reducer queue gate keeps RDS
// posture node-property updates waiting until the same scope generation's
// CloudResource nodes have committed. The handler also gates, but queue-level
// blocking avoids noisy retryable failures while #805 node writes are pending.
type rdsPostureReadinessQueueDB struct {
	now          time.Time
	phaseReady   bool
	status       string
	attemptCount int
}

func (db *rdsPostureReadinessQueueDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeResult{}, nil
}

func (db *rdsPostureReadinessQueueDB) QueryContext(_ context.Context, query string, _ ...any) (Rows, error) {
	if !strings.Contains(query, "FROM fact_work_items") || !strings.Contains(query, "FROM claimed") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	if !strings.Contains(query, "rds_posture_materialization") {
		return nil, fmt.Errorf("claim query missing rds posture readiness gate:\n%s", query)
	}
	hasReadinessGate := queryHasBoundedReadinessRequirement(
		query,
		string(reducer.DomainRDSPostureMaterialization),
		"cloud_resource_uid",
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
		"reducer-rds-posture-1",
		"aws:111111111111:us-east-1:rds",
		"gen-aws-1",
		string(reducer.DomainRDSPostureMaterialization),
		db.attemptCount + 1,
		db.now.Add(-time.Minute),
		db.now.Add(-time.Minute),
		[]byte(`{"entity_key":"aws_resource_materialization:aws:111111111111:us-east-1:rds","reason":"rds posture observed","fact_id":"fact-rds-posture-1","source_system":"aws"}`),
	}}}, nil
}

func TestReducerQueueClaimWaitsForRDSPostureReadinessBehavior(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 1, 14, 0, 0, 0, time.UTC)
	db := &rdsPostureReadinessQueueDB{
		now:        now,
		phaseReady: false,
		status:     "pending",
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
	if claimed {
		t.Fatalf("Claim() claimed %q before canonical readiness, want unclaimed waiting work", intent.IntentID)
	}

	db.phaseReady = true
	intent, claimed, err = queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() after readiness error = %v", err)
	}
	if !claimed {
		t.Fatal("Claim() after readiness claimed = false, want true")
	}
	if got, want := intent.Domain, reducer.DomainRDSPostureMaterialization; got != want {
		t.Fatalf("claimed domain = %q, want %q", got, want)
	}
	if got, want := intent.EntityKeys, []string{"aws_resource_materialization:aws:111111111111:us-east-1:rds"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("claimed entity keys = %v, want %v", got, want)
	}
}
