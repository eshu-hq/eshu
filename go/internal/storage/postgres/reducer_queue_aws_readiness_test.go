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

func TestReducerQueueClaimWaitsForAWSRelationshipReadinessBehavior(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 31, 10, 10, 0, 0, time.UTC)
	db := &awsRelationshipReadinessQueueDB{
		now:        now,
		phaseReady: false,
		item: awsRelationshipQueueItem{
			status:       "pending",
			attemptCount: 0,
		},
	}
	queue := awsRelationshipReadinessQueue(db, now)

	intent, claimed, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if claimed {
		t.Fatalf("Claim() claimed %q before canonical readiness, want unclaimed waiting work", intent.IntentID)
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
		t.Fatal("Claim() after readiness claimed = false, want true")
	}
	if got, want := intent.Domain, reducer.DomainAWSRelationshipMaterialization; got != want {
		t.Fatalf("claimed domain = %q, want %q", got, want)
	}
	if got, want := intent.EntityKeys, []string{"aws_resource_materialization:aws:123456789012:us-east-1:lambda"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("claimed entity keys = %v, want %v", got, want)
	}
}

func TestReducerQueueClaimWaitsForRetryingAWSRelationshipReadinessBehavior(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 31, 10, 20, 0, 0, time.UTC)
	db := &awsRelationshipReadinessQueueDB{
		now:        now,
		phaseReady: false,
		item: awsRelationshipQueueItem{
			status:       "retrying",
			attemptCount: 2,
		},
	}
	queue := awsRelationshipReadinessQueue(db, now)

	if intent, claimed, err := queue.Claim(context.Background()); err != nil {
		t.Fatalf("Claim() error = %v", err)
	} else if claimed {
		t.Fatalf("Claim() claimed retrying intent %q before readiness, want waiting", intent.IntentID)
	}

	db.phaseReady = true
	intent, claimed, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() after readiness error = %v", err)
	}
	if !claimed {
		t.Fatal("Claim() after readiness claimed = false, want retrying item claimable")
	}
	if got, want := intent.AttemptCount, 3; got != want {
		t.Fatalf("claimed attempt count = %d, want retry claim attempt %d", got, want)
	}
}

func TestReducerQueueClaimAWSRelationshipAlreadyReadyBehavior(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 31, 10, 25, 0, 0, time.UTC)
	db := &awsRelationshipReadinessQueueDB{
		now:        now,
		phaseReady: true,
		item: awsRelationshipQueueItem{
			status:       "pending",
			attemptCount: 0,
		},
	}
	queue := awsRelationshipReadinessQueue(db, now)

	intent, claimed, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if !claimed {
		t.Fatal("Claim() claimed = false, want already-ready relationship item claimable")
	}
	if got, want := intent.AttemptCount, 1; got != want {
		t.Fatalf("claimed attempt count = %d, want first claim attempt %d", got, want)
	}
	if got, want := intent.Domain, reducer.DomainAWSRelationshipMaterialization; got != want {
		t.Fatalf("claimed domain = %q, want %q", got, want)
	}
}

func awsRelationshipReadinessQueue(db *awsRelationshipReadinessQueueDB, now time.Time) ReducerQueue {
	return ReducerQueue{
		db:            db,
		LeaseOwner:    "test-owner",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
}

type awsRelationshipQueueItem struct {
	status       string
	attemptCount int
}

type awsRelationshipReadinessQueueDB struct {
	now          time.Time
	phaseReady   bool
	item         awsRelationshipQueueItem
	claimQueries int
}

func (db *awsRelationshipReadinessQueueDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeResult{}, nil
}

func (db *awsRelationshipReadinessQueueDB) QueryContext(_ context.Context, query string, _ ...any) (Rows, error) {
	if !strings.Contains(query, "FROM fact_work_items") || !strings.Contains(query, "FROM claimed") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	db.claimQueries++

	hasReadinessGate := queryHasBoundedReadinessRequirement(
		query,
		string(reducer.DomainAWSRelationshipMaterialization),
		"cloud_resource_uid",
		"canonical_nodes_committed",
	) && queryHasPayloadReadinessLookup(query, "fact_work_items", "readiness_req", "readiness_phase")
	if hasReadinessGate && !db.phaseReady {
		return &queueFakeRows{}, nil
	}

	status := strings.TrimSpace(db.item.status)
	if status == "" {
		status = "pending"
	}
	if status != "pending" && status != "retrying" {
		return &queueFakeRows{}, nil
	}

	return &queueFakeRows{rows: [][]any{{
		"reducer-aws-rel-1",
		"aws:123456789012:us-east-1:lambda",
		"gen-aws-1",
		string(reducer.DomainAWSRelationshipMaterialization),
		db.item.attemptCount + 1,
		db.now.Add(-time.Minute),
		db.now.Add(-time.Minute),
		[]byte(`{"entity_key":"aws_resource_materialization:aws:123456789012:us-east-1:lambda","reason":"aws runtime relationship facts observed","fact_id":"fact-rel-1","source_system":"aws"}`),
	}}}, nil
}
