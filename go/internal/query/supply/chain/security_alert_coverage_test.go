// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Fixture identities. Every name is a placeholder: example-org/example-repo is
// the requested repository, example-org/other-repo sits only in another
// target's allowlist, and the sa-target-* values are target scope ids that must
// never reach a payload.
const (
	coverageTestRepositoryID = "repository:r_example"
	coverageTestRepoTarget   = `{"targets":[{"provider":"github_dependabot","scope":"repository","scope_id":"sa-target-repo","repository":"Example-Org/Example-Repo","allowed_repositories":["example-org/example-repo"]}]}`
	coverageTestOrgTarget    = `{"targets":[{"provider":"github_dependabot","scope":"organization","scope_id":"sa-target-org","organization":"example-org","allowed_repositories":["example-org/other-repo"]}]}`
)

// fakeSecurityAlertCollectionState is a SecurityAlertCollectionStateStore
// double that records every call so a test can prove the probe never runs on a
// non-empty answer or an out-of-grant selector.
type fakeSecurityAlertCollectionState struct {
	mu        sync.Mutex
	configs   []string
	states    map[string]SecurityAlertTargetState
	configErr error
	stateErr  error
	// block makes the configuration read wait for its context, so a test can
	// prove the probe deadline fails closed.
	block        bool
	configCalls  int
	stateCalls   int
	lastScopeIDs []string
}

func (f *fakeSecurityAlertCollectionState) SecurityAlertCollectorConfigurations(ctx context.Context) ([]string, error) {
	f.mu.Lock()
	f.configCalls++
	block := f.block
	f.mu.Unlock()
	if block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.configErr != nil {
		return nil, f.configErr
	}
	return append([]string(nil), f.configs...), nil
}

func (f *fakeSecurityAlertCollectionState) SecurityAlertTargetStates(
	_ context.Context,
	scopeIDs []string,
) ([]SecurityAlertTargetState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stateCalls++
	f.lastScopeIDs = append([]string(nil), scopeIDs...)
	if f.stateErr != nil {
		return nil, f.stateErr
	}
	out := make([]SecurityAlertTargetState, 0, len(scopeIDs))
	for _, id := range scopeIDs {
		if state, ok := f.states[id]; ok {
			state.ScopeID = id
			out = append(out, state)
		}
	}
	return out, nil
}

func (f *fakeSecurityAlertCollectionState) calls() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.configCalls, f.stateCalls
}

func (f *fakeSecurityAlertCollectionState) scopeIDs() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.lastScopeIDs, ",")
}

// coverageRowsStore serves the list, count, and inventory reads with a fixed
// number of rows so a case can choose an empty or a non-empty answer.
type coverageRowsStore struct {
	rows int
}

func (s coverageRowsStore) ListSecurityAlertReconciliations(
	context.Context, SecurityAlertReconciliationFilter,
) ([]SecurityAlertReconciliationRow, error) {
	out := make([]SecurityAlertReconciliationRow, 0, s.rows)
	for i := 0; i < s.rows; i++ {
		out = append(out, SecurityAlertReconciliationRow{ReconciliationID: "reconciliation-1", SourceFreshness: "fresh"})
	}
	return out, nil
}

func (s coverageRowsStore) CountSecurityAlertReconciliations(
	context.Context, SecurityAlertReconciliationAggregateFilter,
) (SecurityAlertReconciliationAggregateCount, error) {
	freshness := map[string]int{}
	if s.rows > 0 {
		freshness["fresh"] = s.rows
	}
	return SecurityAlertReconciliationAggregateCount{
		TotalReconciliations:   s.rows,
		ByReconciliationStatus: map[string]int{},
		ByProvider:             map[string]int{},
		ByProviderState:        map[string]int{},
		BySourceFreshness:      freshness,
	}, nil
}

func (s coverageRowsStore) SecurityAlertReconciliationInventory(
	context.Context, SecurityAlertReconciliationAggregateFilter, SecurityAlertReconciliationInventoryDimension, int, int,
) ([]SecurityAlertReconciliationInventoryRow, error) {
	out := make([]SecurityAlertReconciliationInventoryRow, 0, s.rows)
	for i := 0; i < s.rows; i++ {
		out = append(out, SecurityAlertReconciliationInventoryRow{Dimension: SecurityAlertReconciliationInventoryByStatus, Value: "matched", Count: 1})
	}
	return out, nil
}

func (coverageRowsStore) SecurityAlertProviderRepositoryScopes(context.Context, string) ([]string, error) {
	return nil, nil
}

func coverageTestHandler(state SecurityAlertCollectionStateStore, rows int) *Handler {
	return &Handler{
		Content: selectorMatchContent{entries: []querycontract.RepositoryCatalogEntry{{
			ID:       coverageTestRepositoryID,
			Name:     "example-repo",
			RepoSlug: "example-org/example-repo",
		}}},
		SecurityAlerts:               coverageRowsStore{rows: rows},
		SecurityAlertAggregates:      coverageRowsStore{rows: rows},
		SecurityAlertCollectionState: state,
	}
}

type coverageResponse struct {
	Data struct {
		Coverage *struct {
			State   string   `json:"state"`
			Reasons []string `json:"reasons"`
		} `json:"coverage"`
		CollectorReadiness *struct {
			State string `json:"readiness_state"`
			Kind  string `json:"collector_kind"`
		} `json:"collector_readiness"`
	} `json:"data"`
	Truth struct {
		Level     string `json:"level"`
		Freshness struct {
			State string `json:"state"`
			Cause string `json:"cause"`
		} `json:"freshness"`
	} `json:"truth"`
}

// coverageRoutes are the three security-alert read routes, each anchored on
// the requested repository.
func coverageRoutes(selector string) map[string]string {
	return map[string]string{
		"list":      "/api/v0/supply-chain/security-alerts/reconciliations?limit=10&repository_id=" + selector,
		"count":     "/api/v0/supply-chain/security-alerts/reconciliations/count?repository_id=" + selector,
		"inventory": "/api/v0/supply-chain/security-alerts/reconciliations/inventory?repository_id=" + selector,
	}
}

func serveCoverage(ctx context.Context, t *testing.T, h *Handler, target string) (int, string, coverageResponse) {
	t.Helper()
	mux := http.NewServeMux()
	h.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, target, nil).WithContext(ctx)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var resp coverageResponse
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode %s: %v; body = %s", target, err, w.Body.String())
		}
	}
	return w.Code, w.Body.String(), resp
}

type coverageWant struct {
	state     string
	reasons   []string
	level     string
	freshness string
	cause     string
	readiness string
}

func terminalState(class string, committed bool) SecurityAlertTargetState {
	return SecurityAlertTargetState{
		LatestWorkItemStatus:  "failed_terminal",
		LatestFailureClass:    class,
		LatestFailureTerminal: true,
		CommittedGeneration:   committed,
	}
}

// TestSecurityAlertEmptyAnswerCoverageInvariant is the #8803 Q3 invariant as a
// table: a security-alert read reports coverage.state=complete only when an
// enabled instance exists, the requested repository is inside a target, and
// that target has a committed generation with no later terminal failure. Every
// other empty answer carries non-empty coverage.reasons and a non-exact or
// non-fresh truth envelope, on the list, count, and inventory routes alike.
func TestSecurityAlertEmptyAnswerCoverageInvariant(t *testing.T) {
	t.Parallel()

	const cause = "missing_collector_completion"
	healthy := SecurityAlertTargetState{LatestWorkItemStatus: "completed", CommittedGeneration: true}
	cases := []struct {
		name    string
		configs []string
		states  map[string]SecurityAlertTargetState
		want    coverageWant
	}{
		{
			name: "collector not configured",
			want: coverageWant{"not_configured", []string{"collector_not_configured"}, "fallback", "unavailable", cause, "not_configured"},
		},
		{
			name:    "repository outside every target",
			configs: []string{coverageTestOrgTarget},
			states:  map[string]SecurityAlertTargetState{"sa-target-org": healthy},
			want:    coverageWant{"not_configured", []string{"repository_outside_target_set"}, "fallback", "unavailable", cause, "not_configured"},
		},
		{
			name:    "target pending with no committed generation",
			configs: []string{coverageTestRepoTarget},
			states:  map[string]SecurityAlertTargetState{"sa-target-repo": {LatestWorkItemStatus: "pending"}},
			want:    coverageWant{"target_incomplete", []string{"no_committed_generation"}, "derived", "building", cause, ""},
		},
		{
			name:    "work items under a different scope id than the config",
			configs: []string{coverageTestRepoTarget},
			states:  map[string]SecurityAlertTargetState{"sa-target-renamed": healthy},
			want:    coverageWant{"target_incomplete", []string{"no_committed_generation"}, "derived", "building", cause, ""},
		},
		{
			name:    "terminal failure and never collected",
			configs: []string{coverageTestRepoTarget},
			states:  map[string]SecurityAlertTargetState{"sa-target-repo": terminalState("auth_denied", false)},
			want:    coverageWant{"target_incomplete", []string{"terminal_fetch_failure:auth_denied"}, "fallback", "unavailable", cause, ""},
		},
		{
			name:    "terminal failure after a committed generation",
			configs: []string{coverageTestRepoTarget},
			states:  map[string]SecurityAlertTargetState{"sa-target-repo": terminalState("not_found", true)},
			want:    coverageWant{"target_incomplete", []string{"terminal_fetch_failure:not_found"}, "derived", "stale", cause, ""},
		},
		{
			name:    "unknown failure class stays bounded",
			configs: []string{coverageTestRepoTarget},
			states:  map[string]SecurityAlertTargetState{"sa-target-repo": terminalState("status 404 from https://example.invalid", false)},
			want:    coverageWant{"target_incomplete", []string{"terminal_fetch_failure:unclassified"}, "fallback", "unavailable", cause, ""},
		},
		{
			// A row written before migration 185 has last_failure_terminal
			// NULL, which the store reads as false: the failed_terminal
			// status alone must still mark the target terminal.
			name:    "failed_terminal status without the terminal flag, never collected",
			configs: []string{coverageTestRepoTarget},
			states: map[string]SecurityAlertTargetState{"sa-target-repo": {
				LatestWorkItemStatus: "failed_terminal", LatestFailureClass: "auth_denied",
			}},
			want: coverageWant{"target_incomplete", []string{"terminal_fetch_failure:auth_denied"}, "fallback", "unavailable", cause, ""},
		},
		{
			name:    "failed_terminal status without the terminal flag after a committed generation",
			configs: []string{coverageTestRepoTarget},
			states: map[string]SecurityAlertTargetState{"sa-target-repo": {
				LatestWorkItemStatus: "failed_terminal", LatestFailureClass: "not_found", CommittedGeneration: true,
			}},
			want: coverageWant{"target_incomplete", []string{"terminal_fetch_failure:not_found"}, "derived", "stale", cause, ""},
		},
		{
			name:    "terminal failure followed by a later success",
			configs: []string{coverageTestRepoTarget},
			states:  map[string]SecurityAlertTargetState{"sa-target-repo": healthy},
			want:    coverageWant{"complete", nil, "exact", "fresh", "", "ready_zero_results"},
		},
		{
			name:    "deactivated org target plus live repo target",
			configs: []string{coverageTestRepoTarget},
			states: map[string]SecurityAlertTargetState{
				"sa-target-repo": healthy,
				"sa-target-org":  terminalState("auth_denied", false),
			},
			want: coverageWant{"complete", nil, "exact", "fresh", "", "ready_zero_results"},
		},
		{
			name: "one covering target failed and another covering target healthy",
			configs: []string{
				coverageTestRepoTarget,
				`{"targets":[{"provider":"github_dependabot","scope":"organization","scope_id":"sa-target-org-covering","organization":"example-org","allowed_repositories":["example-org/example-repo"]}]}`,
			},
			states: map[string]SecurityAlertTargetState{
				"sa-target-repo":         terminalState("auth_denied", false),
				"sa-target-org-covering": healthy,
			},
			want: coverageWant{"complete", nil, "exact", "fresh", "", "ready_zero_results"},
		},
		{
			name:    "org allowlist names the repository under another owner",
			configs: []string{`{"targets":[{"provider":"github_dependabot","scope":"organization","scope_id":"sa-target-org-other","organization":"another-org","allowed_repositories":["example-org/example-repo"]}]}`},
			states:  map[string]SecurityAlertTargetState{"sa-target-org-other": healthy},
			want:    coverageWant{"not_configured", []string{"repository_outside_target_set"}, "fallback", "unavailable", cause, "not_configured"},
		},
		{
			name:    "healthy zero control",
			configs: []string{coverageTestRepoTarget, coverageTestOrgTarget},
			states:  map[string]SecurityAlertTargetState{"sa-target-repo": healthy, "sa-target-org": healthy},
			want:    coverageWant{"complete", nil, "exact", "fresh", "", "ready_zero_results"},
		},
	}
	for _, tc := range cases {
		for route, target := range coverageRoutes(coverageTestRepositoryID) {
			t.Run(tc.name+"/"+route, func(t *testing.T) {
				t.Parallel()
				store := &fakeSecurityAlertCollectionState{configs: tc.configs, states: tc.states}
				code, body, resp := serveCoverage(context.Background(), t, coverageTestHandler(store, 0), target)
				if code != http.StatusOK {
					t.Fatalf("status = %d, want 200; body = %s", code, body)
				}
				assertCoverage(t, resp, tc.want)
				assertNoTargetIdentity(t, body)
				assertCoverageInvariant(t, resp)
			})
		}
	}
}

func assertCoverage(t *testing.T, resp coverageResponse, want coverageWant) {
	t.Helper()
	if resp.Data.Coverage == nil {
		t.Fatalf("coverage missing from response")
	}
	if got := resp.Data.Coverage.State; got != want.state {
		t.Fatalf("coverage.state = %q, want %q", got, want.state)
	}
	if got := strings.Join(resp.Data.Coverage.Reasons, ","); got != strings.Join(want.reasons, ",") {
		t.Fatalf("coverage.reasons = %q, want %q", got, strings.Join(want.reasons, ","))
	}
	if got := resp.Truth.Level; got != want.level {
		t.Fatalf("truth.level = %q, want %q", got, want.level)
	}
	if got := resp.Truth.Freshness.State; got != want.freshness {
		t.Fatalf("truth.freshness.state = %q, want %q", got, want.freshness)
	}
	if got := resp.Truth.Freshness.Cause; got != want.cause {
		t.Fatalf("truth.freshness.cause = %q, want %q", got, want.cause)
	}
	gotReadiness := ""
	if resp.Data.CollectorReadiness != nil {
		gotReadiness = resp.Data.CollectorReadiness.State
		if resp.Data.CollectorReadiness.Kind != "security_alert" {
			t.Fatalf("collector_readiness.collector_kind = %q, want security_alert", resp.Data.CollectorReadiness.Kind)
		}
	}
	if gotReadiness != want.readiness {
		t.Fatalf("collector_readiness.readiness_state = %q, want %q", gotReadiness, want.readiness)
	}
}

// assertCoverageInvariant is the Q3 invariant on its own: any answer that is
// not complete must say why and must not read as exact and fresh.
func assertCoverageInvariant(t *testing.T, resp coverageResponse) {
	t.Helper()
	if resp.Data.Coverage == nil || resp.Data.Coverage.State == "complete" {
		return
	}
	if len(resp.Data.Coverage.Reasons) == 0 {
		t.Fatalf("incomplete coverage %q carries no reasons", resp.Data.Coverage.State)
	}
	if resp.Truth.Level == "exact" && resp.Truth.Freshness.State == "fresh" {
		t.Fatalf("incomplete coverage %q reads as exact and fresh", resp.Data.Coverage.State)
	}
}

// assertNoTargetIdentity fails when a payload names a target scope id, a
// provider scope id, an allowlist, or another repository.
func assertNoTargetIdentity(t *testing.T, body string) {
	t.Helper()
	for _, leak := range []string{"sa-target", "security-alert:github", "other-repo", "allowed_repositories", "example-org/"} {
		if strings.Contains(body, leak) {
			t.Fatalf("response leaks %q: %s", leak, body)
		}
	}
}
