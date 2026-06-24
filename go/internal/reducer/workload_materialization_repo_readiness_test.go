// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// keyedPhaseLookup answers readiness from an in-memory set of exact phase keys,
// so a test exercises the real (scope, acceptance_unit, source_run, generation,
// keyspace) match the production Postgres lookup performs — unlike the
// always-ready stubs, it returns found only for keys actually published.
type keyedPhaseLookup struct {
	ready map[GraphProjectionPhaseKey]GraphProjectionPhase
}

func (l keyedPhaseLookup) lookup(key GraphProjectionPhaseKey, phase GraphProjectionPhase) (bool, bool) {
	got, ok := l.ready[key]
	if !ok || got != phase {
		return false, false
	}
	return true, true
}

// TestWorkloadMaterializationRepoReadinessKeyRoundTrips proves the publisher and
// consumer derive the SAME readiness key from the same (scope, repo, generation)
// triple, so the code-stage handles_route/runs_in intent can find the
// workload-stage phase row without knowing the workload stage's source_run.
func TestWorkloadMaterializationRepoReadinessKeyRoundTrips(t *testing.T) {
	t.Parallel()

	scopeID := "scope-a"
	repoID := "repository:r_858f75b9"
	generationID := "460ece2548"

	publisherKey := workloadMaterializationRepoReadinessKey(scopeID, repoID, generationID)
	consumerKey := workloadMaterializationRepoReadinessKey(scopeID, repoID, generationID)

	if publisherKey != consumerKey {
		t.Fatalf("publisher key %+v != consumer key %+v", publisherKey, consumerKey)
	}
	if publisherKey.AcceptanceUnitID != repoID {
		t.Fatalf("acceptance_unit = %q, want repo id %q", publisherKey.AcceptanceUnitID, repoID)
	}
	if publisherKey.SourceRunID != generationID {
		t.Fatalf("source_run = %q, want generation %q (cross-stage source_run can never align)", publisherKey.SourceRunID, generationID)
	}
	if publisherKey.GenerationID != generationID {
		t.Fatalf("generation = %q, want %q", publisherKey.GenerationID, generationID)
	}
	if publisherKey.Keyspace != GraphProjectionKeyspaceServiceUID {
		t.Fatalf("keyspace = %q, want %q", publisherKey.Keyspace, GraphProjectionKeyspaceServiceUID)
	}
	if err := publisherKey.Validate(); err != nil {
		t.Fatalf("readiness key invalid: %v", err)
	}
}

// TestFilterRowsByReadinessHandlesRouteResolvesViaRepoKey reproduces the live
// #2891 mismatch and proves the repo-keyed readiness key fixes it: the
// handles_route intent's acceptance unit (repository:r_858f75b9) and source_run
// (a CODE-stage run) differ from the workload stage's, so the old
// graphProjectionPhaseKeyForIntent lookup misses forever. The ONLY published
// phase row is the repo-keyed one (au=repository:r_858f75b9, srun=generation),
// which the consumer must reconstruct and match.
func TestFilterRowsByReadinessHandlesRouteResolvesViaRepoKey(t *testing.T) {
	t.Parallel()

	scopeID := "scope-a"
	repoID := "repository:r_858f75b9"
	generationID := "460ece2548"

	now := time.Date(2026, time.June, 17, 12, 0, 0, 0, time.UTC)
	row := SharedProjectionIntentRow{
		IntentID:         "hr-1",
		ProjectionDomain: DomainHandlesRoute,
		PartitionKey:     "hr-1",
		ScopeID:          scopeID,
		AcceptanceUnitID: repoID,
		RepositoryID:     repoID,
		// CODE-stage source_run, distinct from the workload stage's generation.
		SourceRunID:  "2e7d1eeaae",
		GenerationID: generationID,
		Payload: map[string]any{
			"function_entity_id": "fn-1",
			"repo_id":            repoID,
			"path":               "/users/{id}",
		},
		CreatedAt: now,
	}

	// Publish ONLY the repo-keyed phase row the workload handler now emits.
	repoKey := workloadMaterializationRepoReadinessKey(scopeID, repoID, generationID)
	lookup := keyedPhaseLookup{ready: map[GraphProjectionPhaseKey]GraphProjectionPhase{
		repoKey: GraphProjectionPhaseWorkloadMaterialization,
	}}

	// Presence has the endpoint committed so the second gate passes it through.
	presence := &fakeRepoPathPresenceLookup{present: map[string]struct{}{
		apiEndpointRepoPathPresenceKey(repoID, "/users/{id}"): {},
	}}

	ready, blocked, terminal, err := filterRowsByReadiness(
		context.Background(), DomainHandlesRoute,
		[]SharedProjectionIntentRow{row}, lookup.lookup, nil, presence,
	)
	if err != nil {
		t.Fatalf("filterRowsByReadiness error = %v", err)
	}
	if len(ready) != 1 || ready[0].IntentID != "hr-1" {
		t.Fatalf("ready = %+v, want the handles_route row resolved via the repo-keyed phase", ready)
	}
	if len(blocked) != 0 {
		t.Fatalf("blocked = %+v, want none (repo-keyed phase row matches)", blocked)
	}
	if len(terminal) != 0 {
		t.Fatalf("terminal = %+v, want none (endpoint present)", terminal)
	}
}

// TestFilterRowsByReadinessHandlesRouteOldStyleKeyStillMisses proves the
// regression's failing case: when ONLY an old-style intent-derived phase row
// exists (au=repo:<id> / srun=gen, the form graphProjectionPhaseStateForIntent
// would have written under the workload intent's keys) — but NOT the repo-keyed
// row — the consumer's repo key does not match, so the handles_route row stays
// deferred rather than silently projecting. This guards that the fix keys on the
// new deterministic row, not on any incidental row.
func TestFilterRowsByReadinessHandlesRouteOldStyleKeyStillMisses(t *testing.T) {
	t.Parallel()

	scopeID := "scope-a"
	repoID := "repository:r_858f75b9"
	generationID := "460ece2548"

	now := time.Date(2026, time.June, 17, 12, 0, 0, 0, time.UTC)
	row := SharedProjectionIntentRow{
		IntentID:         "hr-1",
		ProjectionDomain: DomainHandlesRoute,
		PartitionKey:     "hr-1",
		ScopeID:          scopeID,
		AcceptanceUnitID: repoID,
		RepositoryID:     repoID,
		SourceRunID:      "2e7d1eeaae",
		GenerationID:     generationID,
		Payload: map[string]any{
			"repo_id": repoID,
			"path":    "/users/{id}",
		},
		CreatedAt: now,
	}

	// Old-style phase row: workload-intent acceptance unit form + srun=gen, which
	// is NOT the repo-keyed row the consumer reconstructs.
	oldStyleKey := GraphProjectionPhaseKey{
		ScopeID:          scopeID,
		AcceptanceUnitID: "repo:r_858f75b9",
		SourceRunID:      generationID,
		GenerationID:     generationID,
		Keyspace:         GraphProjectionKeyspaceServiceUID,
	}
	lookup := keyedPhaseLookup{ready: map[GraphProjectionPhaseKey]GraphProjectionPhase{
		oldStyleKey: GraphProjectionPhaseWorkloadMaterialization,
	}}
	presence := &fakeRepoPathPresenceLookup{present: map[string]struct{}{}}

	ready, blocked, _, err := filterRowsByReadiness(
		context.Background(), DomainHandlesRoute,
		[]SharedProjectionIntentRow{row}, lookup.lookup, nil, presence,
	)
	if err != nil {
		t.Fatalf("filterRowsByReadiness error = %v", err)
	}
	if len(ready) != 0 {
		t.Fatalf("ready = %+v, want none (only a non-matching old-style row exists)", ready)
	}
	if len(blocked) != 1 {
		t.Fatalf("blocked = %+v, want the row deferred (repo key does not match old-style row)", blocked)
	}
}

// TestFilterRowsByReadinessCodeCallsKeyUnchanged guards that a non-symbol-runtime
// domain (code_calls) keeps deriving its readiness key from
// graphProjectionPhaseKeyForIntent, byte-identical to its pre-#2891 behavior:
// the intent-derived key resolves it, and the repo-keyed row does NOT.
func TestFilterRowsByReadinessCodeCallsKeyUnchanged(t *testing.T) {
	t.Parallel()

	scopeID := "scope-a"
	repoID := "repository:r_858f75b9"
	generationID := "gen-1"

	row := SharedProjectionIntentRow{
		IntentID:         "cc-1",
		ProjectionDomain: DomainCodeCalls,
		PartitionKey:     "cc-1",
		ScopeID:          scopeID,
		AcceptanceUnitID: repoID,
		RepositoryID:     repoID,
		SourceRunID:      generationID,
		GenerationID:     generationID,
		Payload:          map[string]any{"repo_id": repoID},
		CreatedAt:        time.Now().UTC(),
	}

	intentKey, ok := graphProjectionPhaseKeyForIntent(row, generationID, GraphProjectionKeyspaceCodeEntitiesUID)
	if !ok {
		t.Fatal("code_calls intent key not derivable")
	}
	lookup := keyedPhaseLookup{ready: map[GraphProjectionPhaseKey]GraphProjectionPhase{
		intentKey: GraphProjectionPhaseCanonicalNodesCommitted,
	}}

	ready, blocked, terminal, err := filterRowsByReadiness(
		context.Background(), DomainCodeCalls,
		[]SharedProjectionIntentRow{row}, lookup.lookup, nil, nil,
	)
	if err != nil {
		t.Fatalf("filterRowsByReadiness error = %v", err)
	}
	if len(ready) != 1 || len(blocked) != 0 || len(terminal) != 0 {
		t.Fatalf("code_calls readiness changed: ready=%d blocked=%d terminal=%d, want 1/0/0", len(ready), len(blocked), len(terminal))
	}

	// The repo-keyed row must NOT resolve code_calls: its key derivation is
	// unchanged, so a row keyed only by the new helper stays blocked.
	repoKey := workloadMaterializationRepoReadinessKey(scopeID, repoID, generationID)
	repoOnly := keyedPhaseLookup{ready: map[GraphProjectionPhaseKey]GraphProjectionPhase{
		repoKey: GraphProjectionPhaseCanonicalNodesCommitted,
	}}
	ready, blocked, _, err = filterRowsByReadiness(
		context.Background(), DomainCodeCalls,
		[]SharedProjectionIntentRow{row}, repoOnly.lookup, nil, nil,
	)
	if err != nil {
		t.Fatalf("filterRowsByReadiness error = %v", err)
	}
	if len(ready) != 0 || len(blocked) != 1 {
		t.Fatalf("code_calls resolved via repo key: ready=%d blocked=%d, want 0/1", len(ready), len(blocked))
	}
}

// TestWorkloadMaterializationHandlerPublishesRepoKeyedPhaseOnCandidatePath proves
// the success path publishes, in addition to the per-EntityKey workload phase row,
// the deterministic per-repo readiness row the handles_route/runs_in consumer
// reconstructs (#2891). The published key must equal the consumer's lookup key for
// the same (scope, repo_id, generation).
func TestWorkloadMaterializationHandlerPublishesRepoKeyedPhaseOnCandidatePath(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	loader := &stubFactLoader{
		envelopes: []facts.Envelope{
			{
				FactID:   "fact-repo",
				FactKind: "repository",
				Payload: map[string]any{
					"graph_id": "repo-payments",
					"name":     "payments",
				},
				ObservedAt: now,
			},
			{
				FactID:   "fact-file",
				FactKind: "file",
				Payload: map[string]any{
					"repo_id": "repo-payments",
					"parsed_file_data": map[string]any{
						"k8s_resources": []any{
							map[string]any{"name": "payments", "kind": "Deployment", "namespace": "production"},
						},
					},
				},
				ObservedAt: now,
			},
		},
	}
	publisher := &recordingGraphProjectionPhasePublisher{}
	handler := WorkloadMaterializationHandler{
		FactLoader:     loader,
		Materializer:   NewWorkloadMaterializer(&recordingCypherExecutor{}),
		PhasePublisher: publisher,
	}

	intent := Intent{
		IntentID:        "intent-wm-repo-key",
		ScopeID:         "scope-payments",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "facts projected",
		EntityKeys:      []string{"repo:payments"}, // basename form, NOT the graph_id
		RelatedScopeIDs: []string{"scope-payments"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	// The consumer's readiness key for a handles_route row in this repo:
	wantKey := workloadMaterializationRepoReadinessKey("scope-payments", "repo-payments", "gen-1")
	if !publishedReadinessKey(publisher, wantKey) {
		t.Fatalf("repo-keyed readiness row %+v not published; calls=%+v", wantKey, publisher.calls)
	}
}

// TestWorkloadMaterializationHandlerPublishesRepoKeyedPhaseOnZeroCandidatePath
// proves a route-only repo (no workload candidate) still gets its repo-keyed
// readiness row, derived from the scope's repository graph_id, so its
// handles_route gate resolves instead of looping forever.
func TestWorkloadMaterializationHandlerPublishesRepoKeyedPhaseOnZeroCandidatePath(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	loader := &stubFactLoader{
		envelopes: []facts.Envelope{
			{
				FactID:   "fact-repo",
				FactKind: "repository",
				Payload: map[string]any{
					"graph_id": "repo-routes-only",
					"name":     "routes-only",
				},
				ObservedAt: now,
			},
		},
	}
	publisher := &recordingGraphProjectionPhasePublisher{}
	handler := WorkloadMaterializationHandler{
		FactLoader:     loader,
		Materializer:   NewWorkloadMaterializer(&recordingCypherExecutor{}),
		PhasePublisher: publisher,
	}

	intent := Intent{
		IntentID:        "intent-wm-zero",
		ScopeID:         "scope-routes",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "facts projected",
		EntityKeys:      []string{"repo:routes-only"},
		RelatedScopeIDs: []string{"scope-routes"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	wantKey := workloadMaterializationRepoReadinessKey("scope-routes", "repo-routes-only", "gen-1")
	if !publishedReadinessKey(publisher, wantKey) {
		t.Fatalf("zero-candidate repo-keyed readiness row %+v not published; calls=%+v", wantKey, publisher.calls)
	}
}

// publishedReadinessKey reports whether any published phase row carries the exact
// readiness key at the workload-materialization phase.
func publishedReadinessKey(p *recordingGraphProjectionPhasePublisher, key GraphProjectionPhaseKey) bool {
	for _, batch := range p.calls {
		for _, row := range batch {
			if row.Key == key && row.Phase == GraphProjectionPhaseWorkloadMaterialization {
				return true
			}
		}
	}
	return false
}

// TestPublishRepoReadinessPhasesRetractsStalePresenceBeforeOpeningGate proves the
// #2891-review fix: opening a repo's workload-materialization phase gate first
// clears that repo's stale (other-generation) presence in BOTH symbol→runtime
// keyspaces. Without it, a generation that materializes no endpoint/workload row
// for a repo (zero-candidate path, or a repo whose endpoints disappeared) leaves
// a prior generation's presence in place, the phase gate opens, and the presence
// second-gate projects a HANDLES_ROUTE/RUNS_IN edge to a stale node instead of
// terminalizing the absent target.
func TestPublishRepoReadinessPhasesRetractsStalePresenceBeforeOpeningGate(t *testing.T) {
	t.Parallel()

	publisher := &recordingGraphProjectionPhasePublisher{}
	presence := &recordingPresenceWriter{}
	repoIDs := []string{"repository:r_1", "repository:r_2"}

	if err := publishRepoReadinessPhases(
		context.Background(), publisher, presence, "scope-1", "gen-1", repoIDs, time.Unix(1700000000, 0).UTC(),
	); err != nil {
		t.Fatalf("publishRepoReadinessPhases error = %v", err)
	}

	// Stale presence retracted for BOTH symbol→runtime keyspaces, scoped to this
	// generation and these repos.
	byKeyspace := make(map[GraphProjectionKeyspace]staleRepoGenerationRetract)
	for _, r := range presence.staleRetract {
		if r.scopeID != "scope-1" || r.generationID != "gen-1" {
			t.Fatalf("stale-retract scope/gen = %s/%s, want scope-1/gen-1", r.scopeID, r.generationID)
		}
		byKeyspace[r.keyspace] = r
	}
	for _, ks := range []GraphProjectionKeyspace{
		GraphProjectionKeyspaceAPIEndpointRepoPath,
		GraphProjectionKeyspaceRepoWorkloadPresence,
	} {
		r, ok := byKeyspace[ks]
		if !ok {
			t.Fatalf("no stale-presence retract for keyspace %q (gate opened without clearing stale presence)", ks)
		}
		if len(r.repoIDs) != 2 {
			t.Fatalf("retract repoIDs for %q = %v, want both repos", ks, r.repoIDs)
		}
	}

	// The readiness rows are still published (the gate still opens).
	if !publishedReadinessKey(publisher, workloadMaterializationRepoReadinessKey("scope-1", "repository:r_1", "gen-1")) {
		t.Fatal("repo readiness phase row not published after retract")
	}

	// A nil presence writer (gate off) skips the retract and stays byte-identical:
	// the readiness rows still publish.
	pub2 := &recordingGraphProjectionPhasePublisher{}
	if err := publishRepoReadinessPhases(
		context.Background(), pub2, nil, "scope-1", "gen-1", repoIDs, time.Unix(1700000000, 0).UTC(),
	); err != nil {
		t.Fatalf("nil-writer error = %v", err)
	}
	if !publishedReadinessKey(pub2, workloadMaterializationRepoReadinessKey("scope-1", "repository:r_1", "gen-1")) {
		t.Fatal("nil-writer path must still publish readiness rows")
	}
}
