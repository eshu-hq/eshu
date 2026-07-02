// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestCommitTakesRepoScopedSharedBarrier proves a generation commit fences only
// against deferred maintenance for its own repository partition, not the whole
// fleet. The commit must take the namespaced two-argument shared advisory lock
// keyed by the committing repository, so a commit for repo A no longer contends
// with maintenance or commits for repo B.
func TestCommitTakesRepoScopedSharedBarrier(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 22, 12, 0, 0, 0, time.UTC)
	db := &fakeTransactionalDB{tx: &fakeTx{}}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	scopeValue := scope.IngestionScope{
		ScopeID:       "scope-A",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-A",
	}
	generation := scope.ScopeGeneration{
		GenerationID: "gen-A",
		ScopeID:      "scope-A",
		ObservedAt:   now.Add(-time.Minute),
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}

	if err := store.CommitScopeGeneration(context.Background(), scopeValue, generation, nil); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	if len(db.tx.execs) == 0 {
		t.Fatal("transaction execs = 0, want repo-scoped shared maintenance barrier lock")
	}
	first := db.tx.execs[0]
	if !strings.Contains(first.query, "pg_advisory_xact_lock_shared") {
		t.Fatalf("first exec = %q, want shared advisory lock", first.query)
	}
	if !strings.Contains(first.query, "hashtext") {
		t.Fatalf("first exec = %q, want namespaced two-arg partitioned lock, not the global key", first.query)
	}
	if got, want := first.args[0], deferredMaintenanceLockNamespace; got != want {
		t.Fatalf("shared barrier namespace = %v, want %v", got, want)
	}
	if got, want := first.args[1], deferredMaintenanceRepoLockKey(scopeValue); got != want {
		t.Fatalf("shared barrier repo key = %v, want %v", got, want)
	}
}

// TestMaintenanceTakesPerRepoExclusiveLocksInOrder proves the leader maintenance
// pass partitions its exclusive lock by source repository instead of holding one
// fleet-wide exclusive lock. Disjoint source repositories acquire disjoint lock
// partitions, so maintenance of repo A does not serialize against repo B. Locks
// are acquired in deterministic sorted order to keep multi-repo acquisition
// deadlock-free.
func TestMaintenanceTakesPerRepoExclusiveLocksInOrder(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 22, 12, 0, 0, 0, time.UTC)
	activeGenerations := [][]any{
		{"repo-zeta", "scope-zeta", "gen-zeta"},
		{"repo-alpha", "scope-alpha", "gen-alpha"},
	}
	tx := &fakeTx{
		queryResponses: []queueFakeRows{
			// Batch transaction re-loads active generations under the batch lock.
			{rows: activeGenerations},
		},
	}
	reopenTx := &fakeTx{
		queryResponses: []queueFakeRows{
			// ReopenDeploymentMappingWorkItems: no succeeded work items.
			{rows: [][]any{}},
		},
	}
	db := &fakeTransactionalDB{
		txs: []*fakeTx{tx, reopenTx},
		queryResponses: []queueFakeRows{
			// Snapshot reads on the store db: catalog, latest facts, active generations.
			{rows: [][]any{
				{[]byte(`{"repo_id":"repo-zeta","name":"zeta"}`)},
				{[]byte(`{"repo_id":"repo-alpha","name":"alpha"}`)},
			}},
			{rows: [][]any{}},
			{rows: activeGenerations},
		},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	if err := store.RunDeferredRelationshipMaintenance(context.Background(), nil, nil); err != nil {
		t.Fatalf("RunDeferredRelationshipMaintenance() error = %v, want nil", err)
	}

	var lockKeys []any
	for _, exec := range tx.execs {
		if strings.Contains(exec.query, "pg_advisory_xact_lock(") &&
			strings.Contains(exec.query, "hashtext") {
			if got, want := exec.args[0], deferredMaintenanceLockNamespace; got != want {
				t.Fatalf("exclusive lock namespace = %v, want %v", got, want)
			}
			lockKeys = append(lockKeys, exec.args[1])
		}
	}
	if len(lockKeys) != 2 {
		t.Fatalf("per-repo exclusive lock count = %d, want 2 (one per active repo)", len(lockKeys))
	}

	// No single global exclusive lock should be taken anymore.
	for _, exec := range tx.execs {
		if exec.query == "SELECT pg_advisory_xact_lock($1)" {
			if got, ok := exec.args[0].(int64); ok && got == deferredMaintenanceBarrierLockKey {
				t.Fatalf("maintenance still takes the fleet-wide global exclusive lock %v", got)
			}
		}
	}

	wantAlpha := deferredMaintenanceRepoLockKeyFromID("repo-alpha")
	wantZeta := deferredMaintenanceRepoLockKeyFromID("repo-zeta")
	if lockKeys[0] != wantAlpha || lockKeys[1] != wantZeta {
		t.Fatalf("lock keys = %v, want sorted [%v %v]", lockKeys, wantAlpha, wantZeta)
	}
}

// TestRepoLockKeyDisjointForDistinctRepos proves distinct repositories map to
// distinct lock partitions and the same repository maps to a stable key, which
// is the property that lets disjoint maintenance run concurrently while keeping
// commit/maintenance fencing correct for a shared repository.
func TestRepoLockKeyDisjointForDistinctRepos(t *testing.T) {
	t.Parallel()

	a := deferredMaintenanceRepoLockKeyFromID("repo-A")
	b := deferredMaintenanceRepoLockKeyFromID("repo-B")
	if a == b {
		t.Fatalf("distinct repos produced equal lock keys: %q", a)
	}
	if a != deferredMaintenanceRepoLockKeyFromID("repo-A") {
		t.Fatal("repo lock key is not stable for the same repo id")
	}
}
