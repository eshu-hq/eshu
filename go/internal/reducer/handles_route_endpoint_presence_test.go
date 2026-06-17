package reducer

import (
	"context"
	"testing"
	"time"
)

// handlesRouteIntentRow builds a minimal DomainHandlesRoute intent row carrying
// the (repo_id, path) the property-keyed Endpoint presence gate keys on.
func handlesRouteIntentRow(intentID, repoID, path string) SharedProjectionIntentRow {
	now := time.Date(2026, time.June, 17, 12, 0, 0, 0, time.UTC)
	return SharedProjectionIntentRow{
		IntentID:         intentID,
		ProjectionDomain: DomainHandlesRoute,
		PartitionKey:     intentID,
		ScopeID:          "scope-a",
		AcceptanceUnitID: repoID,
		RepositoryID:     repoID,
		SourceRunID:      "run-1",
		GenerationID:     "gen-1",
		Payload: map[string]any{
			"function_entity_id": "fn-" + intentID,
			"repo_id":            repoID,
			"path":               path,
		},
		CreatedAt: now,
	}
}

func TestAPIEndpointRepoPathPresenceKeyShape(t *testing.T) {
	t.Parallel()

	got := apiEndpointRepoPathPresenceKey("repo-1", "/users/{id}")
	want := "repo-1\x00/users/{id}"
	if got != want {
		t.Fatalf("presence key = %q, want %q", got, want)
	}
	// Blank repo_id or path cannot key a presence row.
	if k := apiEndpointRepoPathPresenceKey("", "/x"); k != "" {
		t.Fatalf("blank repo_id key = %q, want empty", k)
	}
	if k := apiEndpointRepoPathPresenceKey("repo-1", ""); k != "" {
		t.Fatalf("blank path key = %q, want empty", k)
	}
}

func TestPublishAPIEndpointRepoPathPresenceUpsertsRepoPathKeys(t *testing.T) {
	t.Parallel()

	writer := &recordingPresenceWriter{}
	rows := []APIEndpointRow{
		{EndpointID: "e1", RepoID: "repo-1", Path: "/a"},
		{EndpointID: "e2", RepoID: "repo-1", Path: "/b"},
		{EndpointID: "e3", RepoID: "", Path: "/skip"},  // blank repo skipped
		{EndpointID: "e4", RepoID: "repo-1", Path: ""}, // blank path skipped
	}
	if err := publishAPIEndpointRepoPathPresence(
		context.Background(), writer, "scope-1", rows, time.Unix(1700000000, 0).UTC(),
	); err != nil {
		t.Fatalf("publish error = %v", err)
	}
	if len(writer.upserts) != 1 {
		t.Fatalf("upsert calls = %d, want 1", len(writer.upserts))
	}
	got := writer.upserts[0]
	if len(got) != 2 {
		t.Fatalf("presence rows = %d, want 2 (blank repo/path skipped)", len(got))
	}
	for _, r := range got {
		if r.Keyspace != GraphProjectionKeyspaceAPIEndpointRepoPath {
			t.Fatalf("keyspace = %q, want %q", r.Keyspace, GraphProjectionKeyspaceAPIEndpointRepoPath)
		}
		if r.ScopeID != "scope-1" || r.UID == "" {
			t.Fatalf("malformed presence row: %+v", r)
		}
	}
}

func TestPublishAPIEndpointRepoPathPresenceNilWriterNoOp(t *testing.T) {
	t.Parallel()

	if err := publishAPIEndpointRepoPathPresence(
		context.Background(), nil, "scope-1",
		[]APIEndpointRow{{EndpointID: "e1", RepoID: "repo-1", Path: "/a"}},
		time.Now().UTC(),
	); err != nil {
		t.Fatalf("nil writer error = %v, want nil", err)
	}
}

// TestFilterRowsByReadinessHandlesRouteDefersAbsentEndpoint proves an absent
// (repo_id, path) Endpoint blocks (defers) the handles_route intent rather than
// projecting it. A blocked row is NOT marked complete by ProcessPartitionOnce —
// it stays pending and is re-enqueued.
func TestFilterRowsByReadinessHandlesRouteDefersAbsentEndpoint(t *testing.T) {
	t.Parallel()

	rows := []SharedProjectionIntentRow{
		handlesRouteIntentRow("present", "repo-1", "/present"),
		handlesRouteIntentRow("absent", "repo-1", "/absent"),
	}
	// Phase gate is satisfied (workload materialization committed) for both.
	phaseReady := func(GraphProjectionPhaseKey, GraphProjectionPhase) (bool, bool) {
		return true, true
	}
	// Presence has only the "/present" endpoint committed.
	presence := &fakeRepoPathPresenceLookup{
		present: map[string]struct{}{
			apiEndpointRepoPathPresenceKey("repo-1", "/present"): {},
		},
	}

	ready, blocked, err := filterRowsByReadiness(
		context.Background(), DomainHandlesRoute, rows, phaseReady, nil, presence,
	)
	if err != nil {
		t.Fatalf("filterRowsByReadiness error = %v", err)
	}
	if len(ready) != 1 || ready[0].IntentID != "present" {
		t.Fatalf("ready = %+v, want only the present-endpoint intent", ready)
	}
	if len(blocked) != 1 || blocked[0].IntentID != "absent" {
		t.Fatalf("blocked = %+v, want only the absent-endpoint intent", blocked)
	}
	if presence.calls == 0 {
		t.Fatal("presence lookup never queried for handles_route")
	}
}

// TestFilterRowsByReadinessHandlesRouteProjectsWhenPresent proves once every
// (repo_id, path) Endpoint is present, all handles_route rows are ready.
func TestFilterRowsByReadinessHandlesRouteProjectsWhenPresent(t *testing.T) {
	t.Parallel()

	rows := []SharedProjectionIntentRow{
		handlesRouteIntentRow("a", "repo-1", "/a"),
		handlesRouteIntentRow("b", "repo-1", "/b"),
	}
	phaseReady := func(GraphProjectionPhaseKey, GraphProjectionPhase) (bool, bool) {
		return true, true
	}
	presence := &fakeRepoPathPresenceLookup{present: map[string]struct{}{
		apiEndpointRepoPathPresenceKey("repo-1", "/a"): {},
		apiEndpointRepoPathPresenceKey("repo-1", "/b"): {},
	}}

	ready, blocked, err := filterRowsByReadiness(
		context.Background(), DomainHandlesRoute, rows, phaseReady, nil, presence,
	)
	if err != nil {
		t.Fatalf("filterRowsByReadiness error = %v", err)
	}
	if len(ready) != 2 {
		t.Fatalf("ready = %d, want 2", len(ready))
	}
	if len(blocked) != 0 {
		t.Fatalf("blocked = %d, want 0", len(blocked))
	}
}

// TestFilterRowsByReadinessHandlesRouteNilPresenceIsTodaysBehavior proves a nil
// presence lookup leaves handles_route behavior byte-identical to today's: the
// phase gate alone decides readiness, no endpoint presence gating.
func TestFilterRowsByReadinessHandlesRouteNilPresenceIsTodaysBehavior(t *testing.T) {
	t.Parallel()

	rows := []SharedProjectionIntentRow{
		handlesRouteIntentRow("a", "repo-1", "/a"),
		handlesRouteIntentRow("b", "repo-1", "/b"),
	}
	phaseReady := func(GraphProjectionPhaseKey, GraphProjectionPhase) (bool, bool) {
		return true, true
	}

	ready, blocked, err := filterRowsByReadiness(
		context.Background(), DomainHandlesRoute, rows, phaseReady, nil, nil,
	)
	if err != nil {
		t.Fatalf("filterRowsByReadiness error = %v", err)
	}
	if len(ready) != 2 || len(blocked) != 0 {
		t.Fatalf("nil presence changed behavior: ready=%d blocked=%d, want 2/0", len(ready), len(blocked))
	}
}

// TestFilterRowsByReadinessNonHandlesRouteIgnoresPresence is the existing-domain
// regression guard: a non-handles_route domain (code_calls) must NOT consult the
// endpoint presence lookup even when one is wired, so all other domains stay
// byte-identical.
func TestFilterRowsByReadinessNonHandlesRouteIgnoresPresence(t *testing.T) {
	t.Parallel()

	rows := []SharedProjectionIntentRow{
		{
			IntentID:         "cc-1",
			ProjectionDomain: DomainCodeCalls,
			PartitionKey:     "pk-1",
			ScopeID:          "scope-a",
			AcceptanceUnitID: "repo-1",
			RepositoryID:     "repo-1",
			SourceRunID:      "run-1",
			GenerationID:     "gen-1",
			Payload:          map[string]any{"repo_id": "repo-1", "path": "/a"},
			CreatedAt:        time.Now().UTC(),
		},
	}
	phaseReady := func(GraphProjectionPhaseKey, GraphProjectionPhase) (bool, bool) {
		return true, true
	}
	// Presence lookup reports EVERYTHING missing; if code_calls consulted it the
	// row would be blocked. It must not.
	presence := &fakeRepoPathPresenceLookup{present: map[string]struct{}{}}

	ready, blocked, err := filterRowsByReadiness(
		context.Background(), DomainCodeCalls, rows, phaseReady, nil, presence,
	)
	if err != nil {
		t.Fatalf("filterRowsByReadiness error = %v", err)
	}
	if len(ready) != 1 || len(blocked) != 0 {
		t.Fatalf("code_calls consulted presence gate: ready=%d blocked=%d, want 1/0", len(ready), len(blocked))
	}
	if presence.calls != 0 {
		t.Fatalf("presence lookup queried for non-handles_route domain: calls=%d", presence.calls)
	}
}

// TestProcessPartitionOnceHandlesRouteDefersAbsentEndpoint proves the deferral
// semantics through the full partition cycle: a handles_route intent whose
// (repo_id, path) Endpoint is absent is NOT marked complete (stays pending,
// re-enqueued) and produces no edge write, while a sibling with a present
// endpoint projects.
func TestProcessPartitionOnceHandlesRouteDefersAbsentEndpoint(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 17, 14, 0, 0, 0, time.UTC)
	reader := &stubSharedIntentReader{
		pending: []SharedProjectionIntentRow{
			handlesRouteIntentRow("present", "repo-1", "/present"),
			handlesRouteIntentRow("absent", "repo-1", "/absent"),
		},
	}
	lease := &stubLeaseManager{claimResult: true}
	edges := &stubEdgeWriter{}
	presence := &fakeRepoPathPresenceLookup{present: map[string]struct{}{
		apiEndpointRepoPathPresenceKey("repo-1", "/present"): {},
	}}

	cfg := PartitionProcessorConfig{
		Domain:         DomainHandlesRoute,
		PartitionID:    0,
		PartitionCount: 1,
		LeaseOwner:     "worker-1",
		LeaseTTL:       30 * time.Second,
		BatchLimit:     100,
		EvidenceSource: handlesRouteEvidenceSource,
	}

	result, err := ProcessPartitionOnce(
		context.Background(), now, cfg, lease, reader, edges,
		acceptedGenerationFixed("gen-1", true), nil,
		readinessLookupFixed(true, true), nil, presence,
	)
	if err != nil {
		t.Fatalf("ProcessPartitionOnce() error = %v", err)
	}
	if result.BlockedReadiness != 1 {
		t.Fatalf("BlockedReadiness = %d, want 1 (absent endpoint)", result.BlockedReadiness)
	}
	// Only the present-endpoint intent is processed/marked complete.
	if len(reader.completedIDs) != 1 || reader.completedIDs[0] != reader.pending[0].IntentID {
		t.Fatalf("completedIDs = %v, want only the present-endpoint intent", reader.completedIDs)
	}
	for _, id := range reader.completedIDs {
		if id == reader.pending[1].IntentID {
			t.Fatalf("absent-endpoint intent was marked complete (drained+dropped): %v", reader.completedIDs)
		}
	}
}

// TestProcessPartitionOnceHandlesRouteNilPresenceProjectsAll proves the
// nil-presence fallback: with no presence lookup wired, handles_route behaves
// exactly as today — the phase gate alone decides, and all phase-ready rows
// project.
func TestProcessPartitionOnceHandlesRouteNilPresenceProjectsAll(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 17, 14, 0, 0, 0, time.UTC)
	reader := &stubSharedIntentReader{
		pending: []SharedProjectionIntentRow{
			handlesRouteIntentRow("a", "repo-1", "/a"),
			handlesRouteIntentRow("b", "repo-1", "/b"),
		},
	}
	lease := &stubLeaseManager{claimResult: true}
	edges := &stubEdgeWriter{}

	cfg := PartitionProcessorConfig{
		Domain:         DomainHandlesRoute,
		PartitionID:    0,
		PartitionCount: 1,
		LeaseOwner:     "worker-1",
		LeaseTTL:       30 * time.Second,
		BatchLimit:     100,
		EvidenceSource: handlesRouteEvidenceSource,
	}

	result, err := ProcessPartitionOnce(
		context.Background(), now, cfg, lease, reader, edges,
		acceptedGenerationFixed("gen-1", true), nil,
		readinessLookupFixed(true, true), nil, nil, // nil presence lookup
	)
	if err != nil {
		t.Fatalf("ProcessPartitionOnce() error = %v", err)
	}
	if result.BlockedReadiness != 0 {
		t.Fatalf("BlockedReadiness = %d, want 0 (nil presence = today's behavior)", result.BlockedReadiness)
	}
	if result.UpsertedRows != 2 {
		t.Fatalf("UpsertedRows = %d, want 2 (both project with nil presence)", result.UpsertedRows)
	}
}

// TestWorkloadMaterializationHandlerPublishesEndpointRepoPathPresence proves the
// workload handler publishes property-keyed (repo_id, path) Endpoint presence
// after the endpoint nodes commit, so the handles_route gate can see them
// (#2809). A nil presence writer is a no-op, leaving the hot workload path
// byte-identical.
func TestWorkloadMaterializationHandlerPublishesEndpointRepoPathPresence(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	inputLoader := &stubWorkloadProjectionInputLoader{
		candidates: []WorkloadCandidate{
			{
				RepoID:         "repo-service-api",
				RepoName:       "service-api",
				Classification: "service",
				Confidence:     0.96,
				Provenance:     []string{"dockerfile_runtime"},
				APIEndpoints: []APIEndpointSignal{
					{Path: "/widgets", Methods: []string{"get"}, SourceKinds: []string{"openapi"}},
				},
			},
		},
	}
	presenceWriter := &recordingPresenceWriter{}
	handler := WorkloadMaterializationHandler{
		FactLoader:             &stubFactLoader{},
		InputLoader:            inputLoader,
		Materializer:           NewWorkloadMaterializer(&recordingCypherExecutor{}),
		EndpointPresenceWriter: presenceWriter,
	}

	intent := Intent{
		IntentID:        "intent-wm-endpoint-presence",
		ScopeID:         "scope-service",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "endpoints ready",
		EntityKeys:      []string{"repo-service-api"},
		RelatedScopeIDs: []string{"scope-service"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(presenceWriter.upserts) == 0 {
		t.Fatal("no endpoint repo/path presence upsert recorded")
	}
	wantUID := apiEndpointRepoPathPresenceKey("repo-service-api", "/widgets")
	found := false
	for _, batch := range presenceWriter.upserts {
		for _, row := range batch {
			if row.Keyspace == GraphProjectionKeyspaceAPIEndpointRepoPath && row.UID == wantUID {
				found = true
				if row.ScopeID != "scope-service" {
					t.Fatalf("presence scope = %q, want scope-service", row.ScopeID)
				}
			}
		}
	}
	if !found {
		t.Fatalf("no presence row for uid %q in %+v", wantUID, presenceWriter.upserts)
	}
}

func TestWorkloadMaterializationHandlerNilPresenceWriterNoOp(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	inputLoader := &stubWorkloadProjectionInputLoader{
		candidates: []WorkloadCandidate{
			{
				RepoID:         "repo-service-api",
				RepoName:       "service-api",
				Classification: "service",
				Confidence:     0.96,
				Provenance:     []string{"dockerfile_runtime"},
				APIEndpoints: []APIEndpointSignal{
					{Path: "/widgets", Methods: []string{"get"}, SourceKinds: []string{"openapi"}},
				},
			},
		},
	}
	handler := WorkloadMaterializationHandler{
		FactLoader:   &stubFactLoader{},
		InputLoader:  inputLoader,
		Materializer: NewWorkloadMaterializer(&recordingCypherExecutor{}),
		// EndpointPresenceWriter nil
	}

	intent := Intent{
		IntentID:        "intent-wm-nil-presence",
		ScopeID:         "scope-service",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "endpoints ready",
		EntityKeys:      []string{"repo-service-api"},
		RelatedScopeIDs: []string{"scope-service"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v (nil presence writer must be a no-op)", err)
	}
	if result.CanonicalWrites == 0 {
		t.Fatal("CanonicalWrites = 0; nil presence writer changed materialization behavior")
	}
}

// fakeRepoPathPresenceLookup answers MissingUIDs from an in-memory present-set
// keyed by the (repo_id, path) synthesized uid.
type fakeRepoPathPresenceLookup struct {
	present map[string]struct{}
	err     error
	calls   int
}

func (l *fakeRepoPathPresenceLookup) MissingUIDs(
	_ context.Context, _ GraphProjectionKeyspace, uids []string,
) ([]string, error) {
	l.calls++
	if l.err != nil {
		return nil, l.err
	}
	var missing []string
	for _, uid := range uids {
		if _, ok := l.present[uid]; !ok {
			missing = append(missing, uid)
		}
	}
	return missing, nil
}
