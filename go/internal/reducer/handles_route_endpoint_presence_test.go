// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
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
	if got == "" {
		t.Fatal("presence key for a valid (repo_id, path) must not be empty")
	}
	// REGRESSION (#2844): the synthesized uid is written to the Postgres text
	// graph_endpoint_presence.uid column. A raw repo_id + "\x00" + path join
	// embeds a NUL byte, which Postgres rejects for text (SQLSTATE 22021),
	// dead-lettering workload materialization for every endpoint-exposing repo.
	// The uid must be Postgres-safe: no NUL byte (and, being a hex digest, no
	// other control bytes).
	if strings.ContainsRune(got, '\x00') {
		t.Fatalf("presence key %q contains a NUL byte; Postgres text columns reject it (SQLSTATE 22021)", got)
	}
	// Deterministic: same (repo_id, path) → same key (publisher and gate must agree).
	if again := apiEndpointRepoPathPresenceKey("repo-1", "/users/{id}"); again != got {
		t.Fatalf("presence key not deterministic: %q vs %q", got, again)
	}
	// Distinct: a different path (or repo) must not collide.
	if apiEndpointRepoPathPresenceKey("repo-1", "/users/{id}") == apiEndpointRepoPathPresenceKey("repo-1", "/users") {
		t.Fatal("distinct paths collided to the same presence key")
	}
	if apiEndpointRepoPathPresenceKey("repo-1", "/x") == apiEndpointRepoPathPresenceKey("repo-2", "/x") {
		t.Fatal("distinct repos collided to the same presence key")
	}
	// The separator boundary must not be ambiguous: ("a","bc") and ("ab","c")
	// must differ even though raw concatenation would not.
	if apiEndpointRepoPathPresenceKey("a", "/bc") == apiEndpointRepoPathPresenceKey("ab", "/c") {
		t.Fatal("separator-ambiguous (repo_id, path) pairs collided")
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
		context.Background(), writer, "scope-1", "gen-1", rows, time.Unix(1700000000, 0).UTC(),
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
		// #2842 provenance: the un-hashed repo_id and the materializing generation
		// are stored so stale rows can be retracted per repo (the uid is a hash).
		if r.RepoID != "repo-1" || r.SourceGeneration != "gen-1" {
			t.Fatalf("row missing #2842 provenance: %+v", r)
		}
	}
	// #2842: stale other-generation rows for the materialized repo are retracted.
	if len(writer.staleRetract) != 1 {
		t.Fatalf("stale-retract calls = %d, want 1", len(writer.staleRetract))
	}
	if sr := writer.staleRetract[0]; sr.keyspace != GraphProjectionKeyspaceAPIEndpointRepoPath ||
		sr.scopeID != "scope-1" || sr.generationID != "gen-1" ||
		len(sr.repoIDs) != 1 || sr.repoIDs[0] != "repo-1" {
		t.Fatalf("stale-retract args = %+v, want api_endpoint_repo_path/scope-1/gen-1/[repo-1]", writer.staleRetract[0])
	}
}

// TestPublishAPIEndpointRepoPathPresenceDeduplicatesRepoPath proves a
// multi-workload repo — several APIEndpointRows sharing one (repo_id, path) but
// distinct workload-scoped endpoint ids — collapses to a single presence row.
// Without the dedupe the batched INSERT ... ON CONFLICT (keyspace, uid) DO UPDATE
// would carry the same conflict key twice and Postgres would reject it, making
// the workload materialization intent retry forever (#2809 review).
func TestPublishAPIEndpointRepoPathPresenceDeduplicatesRepoPath(t *testing.T) {
	t.Parallel()

	writer := &recordingPresenceWriter{}
	rows := []APIEndpointRow{
		{EndpointID: "wl-1:/orders", RepoID: "repo-1", Path: "/orders"},
		{EndpointID: "wl-2:/orders", RepoID: "repo-1", Path: "/orders"}, // same repo+path, different workload
		{EndpointID: "wl-3:/health", RepoID: "repo-1", Path: "/health"},
	}
	if err := publishAPIEndpointRepoPathPresence(
		context.Background(), writer, "scope-1", "gen-1", rows, time.Unix(1700000000, 0).UTC(),
	); err != nil {
		t.Fatalf("publish error = %v", err)
	}
	if len(writer.upserts) != 1 {
		t.Fatalf("upsert calls = %d, want 1", len(writer.upserts))
	}
	got := writer.upserts[0]
	if len(got) != 2 {
		t.Fatalf("presence rows = %d, want 2 (the /orders duplicate collapsed)", len(got))
	}
	seen := map[string]int{}
	for _, r := range got {
		seen[r.UID]++
	}
	for uid, count := range seen {
		if count != 1 {
			t.Fatalf("uid %q appeared %d times in one upsert batch, want 1 (dedupe)", uid, count)
		}
	}
	if seen[apiEndpointRepoPathPresenceKey("repo-1", "/orders")] != 1 ||
		seen[apiEndpointRepoPathPresenceKey("repo-1", "/health")] != 1 {
		t.Fatalf("expected one /orders and one /health presence row, got %+v", seen)
	}
}

func TestPublishAPIEndpointRepoPathPresenceNilWriterNoOp(t *testing.T) {
	t.Parallel()

	if err := publishAPIEndpointRepoPathPresence(
		context.Background(), nil, "scope-1", "gen-1",
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
