// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// TestBoundedRepositoryGenerationReadsMatchShippedRead pins the derived
// repo-bounded and partition-bounded reads (#7584) to the shipped corpus-wide
// activeRepositoryGenerationsQuery: for every subset, their rows equal the
// shipped rows filtered in Go. The corpus includes the shapes where a
// hand-written bound would diverge: two scopes deriving the same repo_id
// (DISTINCT ON keeps one), two repositories in one partition, a scope whose
// newest generation is pending with no active pointer, a superseded
// generation, and a cloud scope with no repository fact.
func TestBoundedRepositoryGenerationReadsMatchShippedRead(t *testing.T) {
	database := openIsolatedBootstrapSchema(t, targetedMaintenanceProofDSN(t), "tgt7584_bounded")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := database.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed %q: %v", query, err)
		}
	}
	scope := func(scopeID, active string) {
		var pointer any
		if active != "" {
			pointer = active
		}
		exec(`INSERT INTO ingestion_scopes
  (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active', $3)`, scopeID, targetedDiffBase, pointer)
	}
	generation := func(scopeID, generationID, status string, offset time.Duration) {
		exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'poll', $3, $3, $4)`, generationID, scopeID, targetedDiffBase.Add(offset), status)
	}
	repoFact := func(factID, scopeID, generationID, payload string, offset time.Duration) {
		exec(`INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'repository', $1, 'git', $1, $4, $4, $5::jsonb)`,
			factID, scopeID, generationID, targetedDiffBase.Add(offset), payload)
	}

	scope("git:a", "a-1")
	generation("git:a", "a-1", "active", 0)
	repoFact("fa", "git:a", "a-1", `{"repo_id":"shared","name":"alpha"}`, time.Minute)
	scope("git:b", "b-1")
	generation("git:b", "b-1", "active", 0)
	repoFact("fb", "git:b", "b-1", `{"graph_id":"shared","name":"beta"}`, 2*time.Minute)
	scope("git:mono", "mono-1")
	generation("git:mono", "mono-1", "active", 0)
	repoFact("fm1", "git:mono", "mono-1", `{"repo_id":"m1","name":"m-one"}`, 0)
	repoFact("fm2", "git:mono", "mono-1", `{"repo_id":"m2","name":"m-two"}`, 0)
	scope("git:pending", "")
	generation("git:pending", "p-1", "pending", 0)
	repoFact("fp", "git:pending", "p-1", `{"repo_id":"pend","name":"pending-repo"}`, 0)
	scope("git:old", "old-2")
	generation("git:old", "old-1", "superseded", 0)
	generation("git:old", "old-2", "active", time.Hour)
	repoFact("fo1", "git:old", "old-1", `{"repo_id":"old","name":"old-repo"}`, 0)
	repoFact("fo2", "git:old", "old-2", `{"repo_id":"old","name":"old-repo"}`, time.Hour)
	scope("gcp:cloud", "c-1")
	generation("gcp:cloud", "c-1", "active", 0)

	adapter := SQLDB{DB: database}
	shipped, err := loadActiveRepositoryGenerations(ctx, adapter)
	if err != nil {
		t.Fatalf("shipped read: %v", err)
	}
	if got := shipped["shared"].ScopeID; got != "git:b" {
		t.Fatalf("fixture invalid: shipped DISTINCT ON keeps %q for the colliding repo_id, want git:b (newest observed_at)", got)
	}

	for _, repoIDs := range [][]string{
		{"shared"}, {"m1"}, {"m1", "m2"}, {"pend", "old"}, {"missing"}, {"shared", "m2", "pend", "old", "missing"},
	} {
		got, err := loadActiveRepositoryGenerationsForRepos(ctx, adapter, repoIDs)
		if err != nil {
			t.Fatalf("bounded read %v: %v", repoIDs, err)
		}
		want := map[string]repositoryGenerationIdentity{}
		for _, repoID := range repoIDs {
			if identity, ok := shipped[repoID]; ok {
				want[repoID] = identity
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("repo-bounded read %v = %v, want shipped rows %v", repoIDs, got, want)
		}
	}

	for _, partitions := range [][]scopeGenerationPartition{
		partitionsOf("git:a", "a-1"),
		partitionsOf("git:b", "b-1"),
		partitionsOf("git:mono", "mono-1"),
		partitionsOf("git:old", "old-1"),
		partitionsOf("git:old", "old-2", "git:pending", "p-1", "gcp:cloud", "c-1"),
	} {
		got, err := loadActiveRepositoryGenerationsForPartitions(ctx, adapter, partitions)
		if err != nil {
			t.Fatalf("partition-bounded read %v: %v", partitions, err)
		}
		want := map[string]repositoryGenerationIdentity{}
		set := partitionSetOf(partitions)
		for repoID, identity := range shipped {
			if _, ok := set[scopeGenerationPartition{ScopeID: identity.ScopeID, GenerationID: identity.GenerationID}]; ok {
				want[repoID] = identity
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("partition-bounded read %v = %v, want shipped rows %v", partitions, got, want)
		}
		t.Logf("partition-bounded %s -> %d repositories", fmt.Sprint(partitions), len(got))
	}
}
