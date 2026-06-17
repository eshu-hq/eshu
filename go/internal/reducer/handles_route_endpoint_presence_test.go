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

// TestFilterRowsByReadinessHandlesRouteTerminatesAbsentEndpoint proves a
// phase-ready handles_route row whose (repo_id, path) Endpoint is absent is
// TERMINAL (complete with no edge), NOT deferred (#2809 liveness). The phase gate
// already proves workload materialization for the repo is done, so an absent
// endpoint will never commit — deferring it would stall the backlog forever.
// Terminal rows are returned separately from blocked rows; only the present row
// stays ready.
func TestFilterRowsByReadinessHandlesRouteTerminatesAbsentEndpoint(t *testing.T) {
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

	ready, blocked, terminal, err := filterRowsByReadiness(
		context.Background(), DomainHandlesRoute, rows, phaseReady, nil, presence,
	)
	if err != nil {
		t.Fatalf("filterRowsByReadiness error = %v", err)
	}
	if len(ready) != 1 || ready[0].IntentID != "present" {
		t.Fatalf("ready = %+v, want only the present-endpoint intent", ready)
	}
	if len(blocked) != 0 {
		t.Fatalf("blocked = %+v, want none (absent endpoint is terminal, not deferred)", blocked)
	}
	if len(terminal) != 1 || terminal[0].IntentID != "absent" {
		t.Fatalf("terminal = %+v, want only the absent-endpoint intent", terminal)
	}
	if presence.calls == 0 {
		t.Fatal("presence lookup never queried for handles_route")
	}
}

// TestFilterRowsByReadinessHandlesRoutePhaseBlockedStaysDeferred proves the
// terminal path applies ONLY after the phase gate passes: a row whose
// workload-materialization phase has NOT committed is deferred (blocked), never
// terminalized, so the presence lookup is not even consulted for it.
func TestFilterRowsByReadinessHandlesRoutePhaseBlockedStaysDeferred(t *testing.T) {
	t.Parallel()

	rows := []SharedProjectionIntentRow{
		handlesRouteIntentRow("waiting", "repo-1", "/waiting"),
	}
	// Phase gate not yet satisfied for any row.
	phaseNotReady := func(GraphProjectionPhaseKey, GraphProjectionPhase) (bool, bool) {
		return false, true
	}
	presence := &fakeRepoPathPresenceLookup{present: map[string]struct{}{}}

	ready, blocked, terminal, err := filterRowsByReadiness(
		context.Background(), DomainHandlesRoute, rows, phaseNotReady, nil, presence,
	)
	if err != nil {
		t.Fatalf("filterRowsByReadiness error = %v", err)
	}
	if len(ready) != 0 {
		t.Fatalf("ready = %+v, want none (phase not committed)", ready)
	}
	if len(blocked) != 1 || blocked[0].IntentID != "waiting" {
		t.Fatalf("blocked = %+v, want the phase-blocked intent deferred", blocked)
	}
	if len(terminal) != 0 {
		t.Fatalf("terminal = %+v, want none (phase-blocked rows are never terminalized)", terminal)
	}
	if presence.calls != 0 {
		t.Fatalf("presence lookup consulted for a phase-blocked row: calls=%d", presence.calls)
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

	ready, blocked, terminal, err := filterRowsByReadiness(
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
	if len(terminal) != 0 {
		t.Fatalf("terminal = %d, want 0 (all endpoints present)", len(terminal))
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

	ready, blocked, terminal, err := filterRowsByReadiness(
		context.Background(), DomainHandlesRoute, rows, phaseReady, nil, nil,
	)
	if err != nil {
		t.Fatalf("filterRowsByReadiness error = %v", err)
	}
	if len(ready) != 2 || len(blocked) != 0 || len(terminal) != 0 {
		t.Fatalf("nil presence changed behavior: ready=%d blocked=%d terminal=%d, want 2/0/0", len(ready), len(blocked), len(terminal))
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

	ready, blocked, terminal, err := filterRowsByReadiness(
		context.Background(), DomainCodeCalls, rows, phaseReady, nil, presence,
	)
	if err != nil {
		t.Fatalf("filterRowsByReadiness error = %v", err)
	}
	if len(ready) != 1 || len(blocked) != 0 || len(terminal) != 0 {
		t.Fatalf("code_calls consulted presence gate: ready=%d blocked=%d terminal=%d, want 1/0/0", len(ready), len(blocked), len(terminal))
	}
	if presence.calls != 0 {
		t.Fatalf("presence lookup queried for non-handles_route domain: calls=%d", presence.calls)
	}
}

// TestProcessPartitionOnceHandlesRouteDrainsAbsentEndpoint proves the terminal
// semantics through the full partition cycle (#2809 liveness): a handles_route
// intent whose (repo_id, path) Endpoint is absent is marked complete (drained,
// NOT re-enqueued) with no edge write, while a sibling with a present endpoint
// projects an edge. The absent row is still retracted (repo-scoped) so a stale
// edge cannot survive, and it is reported as terminal-no-endpoint, never blocked.
func TestProcessPartitionOnceHandlesRouteDrainsAbsentEndpoint(t *testing.T) {
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
	if result.BlockedReadiness != 0 {
		t.Fatalf("BlockedReadiness = %d, want 0 (absent endpoint is terminal, not deferred)", result.BlockedReadiness)
	}
	if result.TerminalNoEndpoint != 1 {
		t.Fatalf("TerminalNoEndpoint = %d, want 1 (the absent-endpoint row)", result.TerminalNoEndpoint)
	}
	// Both intents are marked complete — the present one as a projected edge, the
	// absent one drained with no edge. The backlog must not retain the absent row.
	completed := map[string]bool{}
	for _, id := range reader.completedIDs {
		completed[id] = true
	}
	if !completed[reader.pending[0].IntentID] || !completed[reader.pending[1].IntentID] {
		t.Fatalf("completedIDs = %v, want both present and absent intents drained", reader.completedIDs)
	}
	// The edge write set contains only the present-endpoint row; the absent row
	// produces no HANDLES_ROUTE edge.
	var writtenIDs []string
	for _, batch := range edges.writeCalls {
		for _, row := range batch {
			writtenIDs = append(writtenIDs, row.IntentID)
		}
	}
	if len(writtenIDs) != 1 || writtenIDs[0] != "present" {
		t.Fatalf("written rows = %v, want only the present-endpoint intent", writtenIDs)
	}
	// The absent row is retracted (repo-scoped cleanup), so no stale edge survives.
	var retractedIDs []string
	for _, batch := range edges.retractCalls {
		for _, row := range batch {
			retractedIDs = append(retractedIDs, row.IntentID)
		}
	}
	if !containsIntentID(retractedIDs, "absent") {
		t.Fatalf("retracted rows = %v, want the absent-endpoint row included for stale-edge cleanup", retractedIDs)
	}
}

func containsIntentID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// TestProcessPartitionOnceHandlesRouteAllTerminalRepoDrainsAndRetracts covers the
// route-only repo: every (repo_id, path) Endpoint is absent, so there are zero
// ready rows. The cycle must still run (not early-return as idle), drain every
// terminal row, and retract repo-scoped so a stale edge from a prior generation
// (when the endpoint existed) is cleared. This is the liveness case that would
// otherwise stall the backlog forever.
func TestProcessPartitionOnceHandlesRouteAllTerminalRepoDrainsAndRetracts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 17, 14, 0, 0, 0, time.UTC)
	reader := &stubSharedIntentReader{
		pending: []SharedProjectionIntentRow{
			handlesRouteIntentRow("route-a", "repo-1", "/a"),
			handlesRouteIntentRow("route-b", "repo-1", "/b"),
		},
	}
	lease := &stubLeaseManager{claimResult: true}
	edges := &stubEdgeWriter{}
	presence := &fakeRepoPathPresenceLookup{present: map[string]struct{}{}} // nothing present

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
	if result.BlockedReadiness != 0 {
		t.Fatalf("BlockedReadiness = %d, want 0 (route-only repo is terminal, not deferred)", result.BlockedReadiness)
	}
	if result.TerminalNoEndpoint != 2 {
		t.Fatalf("TerminalNoEndpoint = %d, want 2 (both routes have no endpoint)", result.TerminalNoEndpoint)
	}
	if result.ProcessedIntents != 2 {
		t.Fatalf("ProcessedIntents = %d, want 2 (both drained, none left pending)", result.ProcessedIntents)
	}
	if len(reader.completedIDs) != 2 {
		t.Fatalf("completedIDs = %v, want both routes drained", reader.completedIDs)
	}
	// No edge written, but the repo is retracted so a stale edge cannot survive.
	for _, batch := range edges.writeCalls {
		if len(batch) != 0 {
			t.Fatalf("write batch = %v, want no HANDLES_ROUTE edge written for an all-terminal repo", batch)
		}
	}
	var retractedIDs []string
	for _, batch := range edges.retractCalls {
		for _, row := range batch {
			retractedIDs = append(retractedIDs, row.IntentID)
		}
	}
	if !containsIntentID(retractedIDs, "route-a") || !containsIntentID(retractedIDs, "route-b") {
		t.Fatalf("retracted rows = %v, want both terminal rows for repo-scoped stale-edge cleanup", retractedIDs)
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
