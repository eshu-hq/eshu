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

// securityGroupReachabilityReadinessQueueDB is a fake reducer-queue backend that
// returns one pending security_group_reachability_materialization intent only
// when the durable SQL claim gate's THREE canonical-nodes keyspaces are all
// present in the query. It proves the gate (not just the in-handler
// ReadinessLookup) holds reachability edge work until the rule, endpoint, and SG
// node phases all commit — the triple-gate is the #1135 PR2b Option D contract.
type securityGroupReachabilityReadinessQueueDB struct {
	now          time.Time
	phaseReady   bool
	attemptCount int
	claimQueries int
}

func (db *securityGroupReachabilityReadinessQueueDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeResult{}, nil
}

func (db *securityGroupReachabilityReadinessQueueDB) QueryContext(_ context.Context, query string, _ ...any) (Rows, error) {
	if !strings.Contains(query, "FROM fact_work_items") || !strings.Contains(query, "FROM claimed") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	db.claimQueries++

	if !strings.Contains(query, "security_group_reachability_materialization") {
		return nil, fmt.Errorf("claim query missing security group reachability readiness gate:\n%s", query)
	}
	// All three keyspaces must be fenced for the edge domain.
	for _, keyspace := range []string{
		"security_group_rule_uid",
		"security_group_endpoint_uid",
		"cloud_resource_uid",
	} {
		if !queryHasBoundedReadinessRequirement(
			query,
			string(reducer.DomainSecurityGroupReachabilityMaterialization),
			keyspace,
			"canonical_nodes_committed",
		) {
			return nil, fmt.Errorf("claim query missing %s keyspace gate:\n%s", keyspace, query)
		}
	}

	hasTripleGate := queryHasBoundedReadinessRequirement(
		query,
		string(reducer.DomainSecurityGroupReachabilityMaterialization),
		"security_group_rule_uid",
		"canonical_nodes_committed",
	) && queryHasBoundedReadinessRequirement(
		query,
		string(reducer.DomainSecurityGroupReachabilityMaterialization),
		"security_group_endpoint_uid",
		"canonical_nodes_committed",
	) && queryHasBoundedReadinessRequirement(
		query,
		string(reducer.DomainSecurityGroupReachabilityMaterialization),
		"cloud_resource_uid",
		"canonical_nodes_committed",
	)
	if hasTripleGate && !db.phaseReady {
		return &queueFakeRows{}, nil
	}

	return &queueFakeRows{rows: [][]any{{
		"reducer-sg-edge-1",
		"aws:111122223333:us-east-1",
		"gen-sg-1",
		string(reducer.DomainSecurityGroupReachabilityMaterialization),
		db.attemptCount + 1,
		db.now.Add(-time.Minute),
		db.now.Add(-time.Minute),
		[]byte(`{"entity_key":"aws_resource_materialization:aws:111122223333:us-east-1","reason":"aws security group rule facts observed","fact_id":"fact-rule-1","source_system":"aws"}`),
	}}}, nil
}

func securityGroupReachabilityReadinessQueue(db *securityGroupReachabilityReadinessQueueDB, now time.Time) ReducerQueue {
	return ReducerQueue{
		db:            db,
		LeaseOwner:    "test-owner",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
}

func TestReducerQueueClaimWaitsForSecurityGroupReachabilityTripleReadiness(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	db := &securityGroupReachabilityReadinessQueueDB{now: now, phaseReady: false}
	queue := securityGroupReachabilityReadinessQueue(db, now)

	intent, claimed, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if claimed {
		t.Fatalf("Claim() claimed %q before all three node phases committed, want unclaimed", intent.IntentID)
	}

	db.phaseReady = true
	intent, claimed, err = queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() after readiness error = %v", err)
	}
	if !claimed {
		t.Fatal("Claim() after triple readiness claimed = false, want true")
	}
	if got, want := intent.Domain, reducer.DomainSecurityGroupReachabilityMaterialization; got != want {
		t.Fatalf("claimed domain = %q, want %q", got, want)
	}
}
