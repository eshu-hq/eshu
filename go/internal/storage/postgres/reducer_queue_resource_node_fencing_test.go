// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestCloudResourceNodeConflictKeyFencesSameResourceSeparatesDistinct proves the
// partition-filtered resource node conflict key (#2782): two AWS resource
// materialization intents for DIFFERENT resources get distinct cloud_resource_node
// conflict keys (so the queue lets them run concurrently), while two intents for
// the SAME resource get the identical key (so the queue serializes them). This is
// the distinct-key-concurrency / same-key-fencing contract for the promoted SAFE
// resource node domain.
func TestCloudResourceNodeConflictKeyFencesSameResourceSeparatesDistinct(t *testing.T) {
	t.Parallel()

	intentFor := func(entityKey string) projector.ReducerIntent {
		return projector.ReducerIntent{
			ScopeID:   "aws:111122223333:us-east-1:ec2",
			Domain:    reducer.DomainAWSResourceMaterialization,
			EntityKey: entityKey,
		}
	}

	domainA, keyA := reducerConflictDomainKey(intentFor("aws_resource_materialization:aws:111122223333:us-east-1:ec2:instance-a"))
	domainB, keyB := reducerConflictDomainKey(intentFor("aws_resource_materialization:aws:111122223333:us-east-1:ec2:instance-b"))
	domainADup, keyADup := reducerConflictDomainKey(intentFor("aws_resource_materialization:aws:111122223333:us-east-1:ec2:instance-a"))

	for _, gotDomain := range []string{domainA, domainB, domainADup} {
		if gotDomain != reducerConflictDomainCloudResourceNode {
			t.Fatalf("conflict domain = %q, want %q (promoted resource node)", gotDomain, reducerConflictDomainCloudResourceNode)
		}
	}
	// Distinct resources => distinct keys => the queue does not fence them.
	if keyA == keyB {
		t.Fatalf("distinct resources share conflict key %q; distinct-key concurrency would be lost", keyA)
	}
	// Same resource => identical key => the queue serializes (same-key fencing),
	// and the key is deterministic across calls.
	if keyA != keyADup {
		t.Fatalf("same resource produced different keys %q and %q; same-key fencing would be lost", keyA, keyADup)
	}
	// The key never leaks the raw provider locator.
	for _, leaked := range []string{"111122223333", "us-east-1", "instance-a"} {
		if strings.Contains(keyA, leaked) {
			t.Fatalf("conflict key %q leaks raw provider value %q", keyA, leaked)
		}
	}
}

// TestReducerQueueClaimAndBatchFenceOnConflictKey proves both the single and the
// batch reducer claim queries fence on (conflict_domain, conflict_key): a work
// item is excluded while another claimed/running item shares its conflict family
// and key. This is the queue mechanism that turns the cloud_resource_node key
// into same-key serialization and distinct-key concurrency.
func TestReducerQueueClaimAndBatchFenceOnConflictKey(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 17, 21, 0, 0, 0, time.UTC)
	conflictFencePredicates := []string{
		"inflight.conflict_domain = fact_work_items.conflict_domain",
		"COALESCE(inflight.conflict_key, inflight.scope_id) = COALESCE(fact_work_items.conflict_key, fact_work_items.scope_id)",
	}

	claimDB := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: nil}}}
	claimQueue := ReducerQueue{
		db:            claimDB,
		LeaseOwner:    "test-owner",
		LeaseDuration: 30 * time.Second,
		Now:           func() time.Time { return now },
	}
	if _, _, err := claimQueue.Claim(context.Background()); err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	claimQuery := claimDB.queries[0].query
	for _, want := range conflictFencePredicates {
		if !strings.Contains(claimQuery, want) {
			t.Fatalf("single claim query missing conflict fence %q:\n%s", want, claimQuery)
		}
	}

	batchDB := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: nil}}}
	batchQueue := ReducerQueue{
		db:            batchDB,
		LeaseOwner:    "test-owner",
		LeaseDuration: 30 * time.Second,
		Now:           func() time.Time { return now },
	}
	if _, err := batchQueue.ClaimBatch(context.Background(), 4); err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}
	batchQuery := batchDB.queries[0].query
	for _, want := range conflictFencePredicates {
		if !strings.Contains(batchQuery, want) {
			t.Fatalf("batch claim query missing conflict fence %q:\n%s", want, batchQuery)
		}
	}
}
