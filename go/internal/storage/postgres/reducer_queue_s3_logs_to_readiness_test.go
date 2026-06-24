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

// s3LogsToReadinessQueueDB is a fake reducer-queue backend that returns one
// pending s3_logs_to_materialization intent only when the canonical-nodes
// readiness gate is satisfied. It proves the durable SQL gate (not just the
// in-handler ReadinessLookup) holds S3 LOGS_TO edge work until the #805 PR1
// CloudResource nodes commit, so a LOGS_TO edge never resolves against an S3
// bucket node that has not committed.
type s3LogsToReadinessQueueDB struct {
	now          time.Time
	phaseReady   bool
	status       string
	attemptCount int
	claimQueries int
}

func (db *s3LogsToReadinessQueueDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeResult{}, nil
}

func (db *s3LogsToReadinessQueueDB) QueryContext(_ context.Context, query string, _ ...any) (Rows, error) {
	if !strings.Contains(query, "FROM fact_work_items") || !strings.Contains(query, "FROM claimed") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	db.claimQueries++

	// The same cloud_resource_uid readiness gate must cover the S3 LOGS_TO
	// domain — both endpoints are S3 CloudResource nodes published under that
	// phase.
	if !strings.Contains(query, "s3_logs_to_materialization") {
		return nil, fmt.Errorf("claim query missing s3 logs-to readiness gate:\n%s", query)
	}

	hasReadinessGate := queryHasBoundedReadinessRequirement(
		query,
		string(reducer.DomainS3LogsToMaterialization),
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
		"reducer-s3-logs-to-1",
		"aws:111111111111:us-east-1:s3",
		"gen-aws-1",
		string(reducer.DomainS3LogsToMaterialization),
		db.attemptCount + 1,
		db.now.Add(-time.Minute),
		db.now.Add(-time.Minute),
		[]byte(`{"entity_key":"aws_resource_materialization:aws:111111111111:us-east-1:s3","reason":"s3 bucket access logging observed","fact_id":"fact-logging-1","source_system":"aws"}`),
	}}}, nil
}

func s3LogsToReadinessQueue(db *s3LogsToReadinessQueueDB, now time.Time) ReducerQueue {
	return ReducerQueue{
		db:            db,
		LeaseOwner:    "test-owner",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
}

func TestReducerQueueClaimWaitsForS3LogsToReadinessBehavior(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 31, 11, 10, 0, 0, time.UTC)
	db := &s3LogsToReadinessQueueDB{
		now:        now,
		phaseReady: false,
		status:     "pending",
	}
	queue := s3LogsToReadinessQueue(db, now)

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
	if got, want := intent.Domain, reducer.DomainS3LogsToMaterialization; got != want {
		t.Fatalf("claimed domain = %q, want %q", got, want)
	}
	if got, want := intent.EntityKeys, []string{"aws_resource_materialization:aws:111111111111:us-east-1:s3"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("claimed entity keys = %v, want %v", got, want)
	}
}
