package reducer

import (
	"context"
	"testing"
	"time"
)

// readinessLookupForKeys returns a key-aware readiness lookup that reports ready
// ONLY for the exact phase keys it was given — unlike readinessLookupFixed, which
// ignores the key. It is what makes the #2892 regression test meaningful: the gate
// must build the SAME key the workload materializer published, or the lookup
// misses.
func readinessLookupForKeys(ready map[GraphProjectionPhaseKey]struct{}) GraphProjectionReadinessLookup {
	return func(key GraphProjectionPhaseKey, _ GraphProjectionPhase) (bool, bool) {
		if _, ok := ready[key]; ok {
			return true, true
		}
		return false, false
	}
}

// TestBridgePhaseGateMatchesRepoWorkloadDoneMarker is the #2892 regression test.
// It reconstructs the REAL published-phase identities a cold compose run produces
// (verified on the eshu2809confirm fixture: the bridge intent carries the
// code-stage acceptance identity, while workload_materialization is published
// under the service-stage AU + generation source run). It proves the readiness
// gate now matches the per-repo workload-done marker
// {scope, repository:<repo_id>, generation, generation, service_uid} instead of
// the intent's own code-stage source run, which never matched. Stubbing the phase
// lookup (readinessLookupFixed) would hide this — only a key-aware lookup catches
// it.
func TestBridgePhaseGateMatchesRepoWorkloadDoneMarker(t *testing.T) {
	t.Parallel()

	const (
		scope      = "git-repository-scope:repository:r_858f75b9"
		repoAU     = "repository:r_858f75b9"
		codeRun    = "2e7d1eeaae15486b8ef1cc9161ec9f6a857db01d91ef8c75349cb1d41f167729"
		generation = "460ece2548709f96a6ec69a273d262205f96cf334392839c55973baffe2504e0"
		path       = "/users"
	)

	// A handles_route intent as emitted by the code stage: AU = repository:<id>,
	// source_run = the code run, generation = the shared generation.
	intent := SharedProjectionIntentRow{
		IntentID:         "hr-1",
		ProjectionDomain: DomainHandlesRoute,
		PartitionKey:     "hr-1",
		ScopeID:          scope,
		AcceptanceUnitID: repoAU,
		RepositoryID:     repoAU,
		SourceRunID:      codeRun,
		GenerationID:     generation,
		Payload:          map[string]any{"repo_id": repoAU, "path": path},
		CreatedAt:        time.Now().UTC(),
	}
	rows := []SharedProjectionIntentRow{intent}

	// Endpoint presence IS committed for (repo, path).
	presence := &fakeRepoPathPresenceLookup{present: map[string]struct{}{
		apiEndpointRepoPathPresenceKey(repoAU, path): {},
	}}

	// Case 1 — the #2892 per-repo workload-done marker is published:
	// {scope, repository:<id>, source_run = generation, generation, service_uid}.
	markerKey := GraphProjectionPhaseKey{
		ScopeID:          scope,
		AcceptanceUnitID: repoAU,
		SourceRunID:      generation, // marker uses the generation as source run
		GenerationID:     generation,
		Keyspace:         GraphProjectionKeyspaceServiceUID,
	}
	ready, blocked, terminal, err := filterRowsByReadiness(
		context.Background(), DomainHandlesRoute, rows,
		readinessLookupForKeys(map[GraphProjectionPhaseKey]struct{}{markerKey: {}}),
		nil, presence,
	)
	if err != nil {
		t.Fatalf("filterRowsByReadiness error = %v", err)
	}
	if len(ready) != 1 || len(blocked) != 0 || len(terminal) != 0 {
		t.Fatalf("with the workload-done marker: ready=%d blocked=%d terminal=%d, want 1/0/0 (edge projects)",
			len(ready), len(blocked), len(terminal))
	}

	// Case 2 — only the stage-native phase exists, keyed by the service-stage AU
	// (workload:<name>) — the exact pre-#2892 production state. The bridge intent
	// must NOT find it (this is the bug: the edge never projects → deferred).
	staleKey := GraphProjectionPhaseKey{
		ScopeID:          scope,
		AcceptanceUnitID: "workload:payments-api",
		SourceRunID:      generation,
		GenerationID:     generation,
		Keyspace:         GraphProjectionKeyspaceServiceUID,
	}
	ready, blocked, terminal, err = filterRowsByReadiness(
		context.Background(), DomainHandlesRoute, rows,
		readinessLookupForKeys(map[GraphProjectionPhaseKey]struct{}{staleKey: {}}),
		nil, presence,
	)
	if err != nil {
		t.Fatalf("filterRowsByReadiness error = %v", err)
	}
	if len(ready) != 0 || len(blocked) != 1 || len(terminal) != 0 {
		t.Fatalf("without the marker: ready=%d blocked=%d terminal=%d, want 0/1/0 (deferred, no false match)",
			len(ready), len(blocked), len(terminal))
	}
}

// TestPublishRepoWorkloadDoneMarkersKeysByRepoAndGeneration proves the workload
// materializer publishes the marker the gate above looks up: one phase per
// distinct repo, keyed by repository:<id> + generation source run under
// service_uid, deduped (#2892).
func TestPublishRepoWorkloadDoneMarkersKeysByRepoAndGeneration(t *testing.T) {
	t.Parallel()

	pub := &recordingPhasePublisher{}
	const scope = "scope-a"
	const generation = "gen-1"
	repoIDs := []string{"repository:r1", "repository:r1", "repository:r2", ""}

	if err := publishRepoWorkloadDoneMarkers(
		context.Background(), pub, scope, generation, repoIDs, time.Unix(1700000000, 0).UTC(),
	); err != nil {
		t.Fatalf("publishRepoWorkloadDoneMarkers error = %v", err)
	}
	if len(pub.states) != 2 {
		t.Fatalf("published %d marker(s), want 2 (deduped, blank dropped)", len(pub.states))
	}
	for _, s := range pub.states {
		if s.Phase != GraphProjectionPhaseWorkloadMaterialization {
			t.Fatalf("phase = %q, want workload_materialization", s.Phase)
		}
		k := s.Key
		if k.ScopeID != scope || k.Keyspace != GraphProjectionKeyspaceServiceUID ||
			k.SourceRunID != generation || k.GenerationID != generation {
			t.Fatalf("marker key = %+v, want scope/service_uid/gen-source-run", k)
		}
		if k.AcceptanceUnitID != "repository:r1" && k.AcceptanceUnitID != "repository:r2" {
			t.Fatalf("marker AU = %q, want a repository:<id>", k.AcceptanceUnitID)
		}
	}

	// Nil publisher / blank scope or generation / empty repos are no-ops.
	if err := publishRepoWorkloadDoneMarkers(context.Background(), nil, scope, generation, repoIDs, time.Now().UTC()); err != nil {
		t.Fatalf("nil publisher error = %v", err)
	}
	if err := publishRepoWorkloadDoneMarkers(context.Background(), pub, "", generation, repoIDs, time.Now().UTC()); err != nil {
		t.Fatalf("blank scope error = %v", err)
	}
	if len(pub.states) != 2 {
		t.Fatalf("no-op calls mutated state: %d markers", len(pub.states))
	}
}
