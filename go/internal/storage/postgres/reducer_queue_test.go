// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestReducerWorkItemIDDeterministic(t *testing.T) {
	t.Parallel()
	intent := runtime.ReducerIntent{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       "workload_identity",
		EntityKey:    "entity-1",
	}
	id1 := reducerWorkItemID(intent)
	id2 := reducerWorkItemID(intent)
	if id1 != id2 {
		t.Fatalf("expected deterministic ID, got %q and %q", id1, id2)
	}
	if !strings.HasPrefix(id1, "reducer_") {
		t.Fatalf("expected prefix 'reducer_', got %q", id1)
	}
}

func TestReducerWorkItemIDSanitizesSpecialChars(t *testing.T) {
	t.Parallel()
	intent := runtime.ReducerIntent{
		ScopeID:      "org/repo",
		GenerationID: "gen:1",
		Domain:       "workload_identity",
		EntityKey:    "entity/key:value",
	}
	id := reducerWorkItemID(intent)
	if strings.Contains(id, "/") || strings.Contains(id, ":") {
		t.Fatalf("ID contains unsanitized chars: %q", id)
	}
}

func TestReducerConflictDomainKeySplitsCodeAndPlatformGraphFamilies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		domain     reducer.Domain
		wantDomain string
		wantKey    string
	}{
		{
			name:       "semantic entities use code graph conflict family",
			domain:     reducer.DomainSemanticEntityMaterialization,
			wantDomain: reducerConflictDomainCodeGraph,
			wantKey:    "scope-1",
		},
		{
			name:       "code call edges use code graph conflict family",
			domain:     reducer.DomainCodeCallMaterialization,
			wantDomain: reducerConflictDomainCodeGraph,
			wantKey:    "scope-1",
		},
		{
			name:       "sql edges use code graph conflict family",
			domain:     reducer.DomainSQLRelationshipMaterialization,
			wantDomain: reducerConflictDomainCodeGraph,
			wantKey:    "scope-1",
		},
		{
			name:       "inheritance edges use code graph conflict family",
			domain:     reducer.DomainInheritanceMaterialization,
			wantDomain: reducerConflictDomainCodeGraph,
			wantKey:    "scope-1",
		},
		{
			// Non-Platform-writing platform-graph domains use per-domain hashed
			// keys (#3672). WorkloadIdentity upserts Postgres fact_records and never
			// MERGEs a :Platform node, so it gets its own key. Exact key value is
			// covered by TestPlatformGraphConflictKeyPartitionsByDomain.
			name:       "workload identity uses platform graph conflict family",
			domain:     reducer.DomainWorkloadIdentity,
			wantDomain: reducerConflictDomainPlatformGraph,
			wantKey:    reducerPlatformGraphConflictKey(reducer.DomainWorkloadIdentity, "scope-1"),
		},
		{
			// PlatformInfraMaterialization MERGEs (p:Platform {id}) (with the
			// PlatformGraphLocker advisory lock); it shares one conflict key with
			// WorkloadMaterialization (which MERGEs the same node WITHOUT the lock) so
			// the queue fence keeps the two unprotected same-node writers serialized
			// (#3672 review P1).
			name:       "platform infra materialization uses shared platform-node-writer key",
			domain:     reducer.DomainPlatformInfraMaterialization,
			wantDomain: reducerConflictDomainPlatformGraph,
			wantKey:    reducerPlatformNodeWriterConflictKey("scope-1"),
		},
		{
			// DeploymentMapping no longer MERGEs a :Platform node, but it requeues
			// workload materialization via reopen-succeeded-only replay, so it shares
			// the scope-keyed platform-node-writer key with WorkloadMaterialization to
			// avoid overlapping a same-scope in-flight workload item (silently-lost
			// replay).
			name:       "deployment mapping shares platform-node-writer key for replay ordering",
			domain:     reducer.DomainDeploymentMapping,
			wantDomain: reducerConflictDomainPlatformGraph,
			wantKey:    reducerPlatformNodeWriterConflictKey("scope-1"),
		},
		{
			name:       "unknown future domains fall back to scope serialization",
			domain:     reducer.DomainOwnership,
			wantDomain: reducerConflictDomainScope,
			wantKey:    "scope-1",
		},
		{
			// The value-flow refresh singleton re-runs the GLOBAL fixpoint
			// (issue #6880): it must serialize against itself on the one
			// global key, never partition per repo.
			name:       "value-flow refresh singleton serializes on the global scope key",
			domain:     reducer.DomainCodeValueFlowRefresh,
			wantDomain: reducerConflictDomainScope,
			wantKey:    "scope-1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotDomain, gotKey := reducerConflictDomainKey(runtime.ReducerIntent{
				ScopeID: " scope-1 ",
				Domain:  tt.domain,
			})
			if gotDomain != tt.wantDomain {
				t.Fatalf("conflict domain = %q, want %q", gotDomain, tt.wantDomain)
			}
			if gotKey != tt.wantKey {
				t.Fatalf("conflict key = %q, want %q", gotKey, tt.wantKey)
			}
		})
	}
}

// TestReducerConflictDomainKeyValueFlowRefreshSingleton is the contention
// proof for issue #6880: the value-flow refresh singleton re-runs the global
// fixpoint retract-and-rewrite, so every refresh item must land on the one
// global conflict key (serializing refresh against itself), while per-repo
// summary items keep disjoint repo-scoped keys (preserving concurrent drain).
// A regression that re-partitioned refresh per repo, or collapsed summaries
// onto the global key, fails here.
func TestReducerConflictDomainKeyValueFlowRefreshSingleton(t *testing.T) {
	t.Parallel()

	refreshKey := func(scope string) (string, string) {
		t.Helper()
		domain, key := reducerConflictDomainKey(runtime.ReducerIntent{
			ScopeID: scope,
			Domain:  reducer.DomainCodeValueFlowRefresh,
		})
		return domain, key
	}

	domain, key := refreshKey("eshu:global")
	if domain != reducerConflictDomainScope || key != "eshu:global" {
		t.Fatalf("refresh conflict = (%q, %q), want (%q, %q)",
			domain, key, reducerConflictDomainScope, "eshu:global")
	}

	// A second refresh item shares the identical key: the claim fence keeps
	// two refresh runs from overlapping the global retract-and-rewrite.
	if domain2, key2 := refreshKey("eshu:global"); domain2 != domain || key2 != key {
		t.Fatalf("second refresh conflict = (%q, %q), want identical (%q, %q)",
			domain2, key2, domain, key)
	}

	// Per-repo summary items never share the global key: they stamp their own
	// repo scope, so summary drain stays concurrent across repos and never
	// contends with the singleton at the queue layer. (Graph-write safety
	// comes from the single-writer shape: summaries write only Postgres
	// snapshots while the fenced singleton alone runs the fixpoint.)
	summaryDomain, summaryKey := reducerConflictDomainKey(runtime.ReducerIntent{
		ScopeID: "repo:abc",
		Domain:  reducer.DomainCodeFunctionSummary,
	})
	if summaryDomain != reducerConflictDomainScope || summaryKey != "repo:abc" {
		t.Fatalf("summary conflict = (%q, %q), want (%q, %q)",
			summaryDomain, summaryKey, reducerConflictDomainScope, "repo:abc")
	}
	if summaryKey == key {
		t.Fatalf("summary key %q collides with the refresh singleton key; per-repo drain would fence on the global fixpoint", summaryKey)
	}
}

func TestReducerQueueBatchEnqueue(t *testing.T) {
	t.Parallel()

	recorder := &reducerRecordingDB{}
	queue := NewReducerQueue(recorder, "test-owner", 30*time.Second)

	// Create 1200 intents to test batching (should use 3 batches: 500 + 500 + 200)
	intents := make([]runtime.ReducerIntent, 1200)
	for i := 0; i < 1200; i++ {
		intents[i] = runtime.ReducerIntent{
			ScopeID:      "scope-1",
			GenerationID: "gen-1",
			Domain:       reducer.DomainWorkloadIdentity,
			EntityKey:    "entity-" + string(rune('a'+i%26)),
			Reason:       "test-reason",
			FactID:       "fact-" + string(rune('a'+i%26)),
			SourceSystem: "test-system",
		}
	}

	ctx := context.Background()
	result, err := queue.Enqueue(ctx, intents)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	if result.Count != 1200 {
		t.Errorf("expected count 1200, got %d", result.Count)
	}

	// Should have called ExecContext 3 times (3 batches: 500 + 500 + 200)
	if recorder.execCount != 3 {
		t.Errorf("expected 3 ExecContext calls for 1200 intents, got %d", recorder.execCount)
	}

	// Verify the queries contain multi-row VALUE clauses (presence of multiple value tuples)
	for i, call := range recorder.execs {
		if !strings.Contains(call.query, "INSERT INTO fact_work_items") {
			t.Errorf("exec[%d] missing INSERT INTO fact_work_items", i)
		}
		if !strings.Contains(call.query, "VALUES") {
			t.Errorf("exec[%d] missing VALUES clause", i)
		}
		// Check that it's a batch by looking for multiple value tuples
		valueCount := strings.Count(call.query, "($")
		expectedSize := reducerEnqueueBatchSize
		if i == 2 {
			expectedSize = 200 // last batch
		}
		if valueCount != expectedSize {
			t.Errorf("exec[%d] has %d value tuples, expected %d", i, valueCount, expectedSize)
		}
	}
}

func TestReducerQueueClaimCanWaitForProjectorDrain(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	queue := ReducerQueue{
		database:                         db,
		LeaseOwner:                       "test-owner",
		LeaseDuration:                    30 * time.Second,
		Now:                              func() time.Time { return now },
		RequireProjectorDrainBeforeClaim: true,
		SemanticEntityClaimLimit:         1,
	}

	_, claimed, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if claimed {
		t.Fatal("Claim() claimed = true, want false from empty rows")
	}

	query := db.queries[0].query
	for _, want := range []string{
		"$5 = false OR NOT EXISTS",
		"projector_work.stage = 'projector'",
		"projector_work.scope_id = fact_work_items.scope_id",
		"projector_work.status IN ('pending', 'retrying', 'claimed', 'running')",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("claim query missing projector drain predicate %q:\n%s", want, query)
		}
	}
	if got, want := db.queries[0].args[4], true; got != want {
		t.Fatalf("projector drain arg = %v, want %v", got, want)
	}
}

func TestReducerQueueClaimGatesSemanticEntitiesOnGlobalProjectorDrain(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	queue := ReducerQueue{
		database:                         db,
		LeaseOwner:                       "test-owner",
		LeaseDuration:                    30 * time.Second,
		Now:                              func() time.Time { return now },
		RequireProjectorDrainBeforeClaim: true,
		SemanticEntityClaimLimit:         1,
	}

	_, claimed, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if claimed {
		t.Fatal("Claim() claimed = true, want false from empty rows")
	}

	query := db.queries[0].query
	for _, want := range []string{
		"domain <> 'semantic_entity_materialization'",
		"projector_any.stage = 'projector'",
		"projector_any.domain = 'source_local'",
		"projector_any.status IN ('pending', 'retrying', 'claimed', 'running')",
		"projector_done.domain = 'source_local'",
		"projector_done.status = 'succeeded'",
		">= $6",
		"semantic_inflight.domain = 'semantic_entity_materialization'",
		"semantic_inflight.status IN ('claimed', 'running')",
		"semantic_inflight.claim_until > $1",
		"< $7",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("claim query missing semantic global projector gate %q:\n%s", want, query)
		}
	}
	if got, want := db.queries[0].args[6], 1; got != want {
		t.Fatalf("semantic claim limit arg = %v, want %v", got, want)
	}
}

func TestReducerQueueClaimPassesExpectedSourceLocalProjectors(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 2, 4, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	queue := ReducerQueue{
		database:                      db,
		LeaseOwner:                    "test-owner",
		LeaseDuration:                 30 * time.Second,
		Now:                           func() time.Time { return now },
		ExpectedSourceLocalProjectors: 878,
	}

	_, _, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if got, want := db.queries[0].args[5], 878; got != want {
		t.Fatalf("expected source-local projector arg = %v, want %v", got, want)
	}
}

func TestReducerQueueClaimPassesSemanticEntityClaimLimit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 2, 15, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	queue := ReducerQueue{
		database:                         db,
		LeaseOwner:                       "test-owner",
		LeaseDuration:                    30 * time.Second,
		Now:                              func() time.Time { return now },
		RequireProjectorDrainBeforeClaim: true,
		SemanticEntityClaimLimit:         4,
	}

	_, _, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if got, want := db.queries[0].args[6], 4; got != want {
		t.Fatalf("semantic claim limit arg = %v, want %v", got, want)
	}
}

func TestReducerQueueClaimGatesAWSRelationshipsOnCanonicalCloudResourceReadiness(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 31, 10, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	queue := ReducerQueue{
		database:      db,
		LeaseOwner:    "test-owner",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}

	_, claimed, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if claimed {
		t.Fatal("Claim() claimed = true, want false from empty rows")
	}

	query := db.queries[0].query
	if !queryHasBoundedReadinessRequirement(
		query,
		string(reducer.DomainAWSRelationshipMaterialization),
		"cloud_resource_uid",
		"canonical_nodes_committed",
	) {
		t.Fatalf("claim query missing AWS relationship readiness requirement:\n%s", query)
	}
	if !queryHasPayloadReadinessLookup(query, "fact_work_items", "readiness_req", "readiness_phase") {
		t.Fatalf("claim query missing AWS relationship payload readiness lookup:\n%s", query)
	}
}

func TestReducerQueueClaimCanFilterByDomain(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 28, 14, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	queue := ReducerQueue{
		database:      db,
		LeaseOwner:    "sql-lane",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
		ClaimDomain:   reducer.DomainSQLRelationshipMaterialization,
	}

	_, claimed, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if claimed {
		t.Fatal("Claim() claimed = true, want false from empty rows")
	}

	got, ok := db.queries[0].args[1].([]string)
	if !ok || len(got) != 1 || got[0] != string(reducer.DomainSQLRelationshipMaterialization) {
		t.Fatalf("domain filter arg = %#v, want [%q]", db.queries[0].args[1], reducer.DomainSQLRelationshipMaterialization)
	}
}

func TestReducerQueueClaimRejectsUnknownDomainFilter(t *testing.T) {
	t.Parallel()

	queue := ReducerQueue{
		database:      &fakeExecQueryer{},
		LeaseOwner:    "bad-lane",
		LeaseDuration: time.Minute,
		ClaimDomain:   reducer.Domain("not_a_domain"),
	}

	_, _, err := queue.Claim(context.Background())
	if err == nil {
		t.Fatal("Claim() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "unknown reducer domain") {
		t.Fatalf("Claim() error = %v, want unknown domain validation", err)
	}
}
