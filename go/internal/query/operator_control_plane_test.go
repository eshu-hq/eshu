package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func operatorControlPlaneRequest(t *testing.T, snapshot statuspkg.RawSnapshot) map[string]any {
	t.Helper()
	handler := &StatusHandler{StatusReader: fakeStatusReader{snapshot: snapshot}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/operator-control-plane", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; body=%s", err, rec.Body.String())
	}
	return payload
}

func operatorHealthState(t *testing.T, payload map[string]any) string {
	t.Helper()
	health, ok := payload["health"].(map[string]any)
	if !ok {
		t.Fatalf("health missing: %#v", payload["health"])
	}
	return health["state"].(string)
}

func TestOperatorControlPlaneHealthy(t *testing.T) {
	t.Parallel()
	payload := operatorControlPlaneRequest(t, statuspkg.RawSnapshot{
		AsOf:  time.Date(2026, 6, 19, 3, 0, 0, 0, time.UTC),
		Queue: statuspkg.QueueSnapshot{Total: 10, Succeeded: 10},
	})
	if got := operatorHealthState(t, payload); got != "healthy" {
		t.Fatalf("health state = %q, want healthy", got)
	}
	// The catalog spine must yield collector families even on a healthy idle run.
	families, ok := payload["collector_families"].([]any)
	if !ok || len(families) == 0 {
		t.Fatalf("collector_families = %#v, want non-empty spine", payload["collector_families"])
	}
	// With no queue failure, latest_failure must be null, not an empty record.
	dead := payload["dead_letters"].(map[string]any)
	if dead["latest_failure"] != nil {
		t.Fatalf("latest_failure = %#v, want null when no failure", dead["latest_failure"])
	}
}

func TestOperatorControlPlaneStuck(t *testing.T) {
	t.Parallel()
	payload := operatorControlPlaneRequest(t, statuspkg.RawSnapshot{
		AsOf: time.Date(2026, 6, 19, 3, 0, 0, 0, time.UTC),
		Queue: statuspkg.QueueSnapshot{
			Outstanding:          6,
			Pending:              6,
			OverdueClaims:        2,
			OldestOutstandingAge: 15 * time.Minute,
		},
		QueueBlockages: []statuspkg.QueueBlockage{
			{Stage: "reducer", Domain: "workload_materialization", Blocked: 2, OldestAge: 10 * time.Minute},
		},
	})
	if got := operatorHealthState(t, payload); got != "stalled" {
		t.Fatalf("health state = %q, want stalled (overdue claims)", got)
	}
	queue := payload["queue"].(map[string]any)
	claim := queue["claim_latency"].(map[string]any)
	if got := claim["overdue_claims"].(float64); got != 2 {
		t.Fatalf("claim_latency.overdue_claims = %v, want 2", got)
	}
	stuck := queue["stuck"].(map[string]any)
	if got := stuck["blocked_conflicts"].(float64); got != 1 {
		t.Fatalf("stuck.blocked_conflicts = %v, want 1", got)
	}
	if got := stuck["oldest_outstanding_age"].(float64); got != (15 * time.Minute).Seconds() {
		t.Fatalf("stuck.oldest_outstanding_age = %v, want 900", got)
	}
}

func TestOperatorControlPlaneDeadLettered(t *testing.T) {
	t.Parallel()
	payload := operatorControlPlaneRequest(t, statuspkg.RawSnapshot{
		AsOf:  time.Date(2026, 6, 19, 3, 0, 0, 0, time.UTC),
		Queue: statuspkg.QueueSnapshot{DeadLetter: 4, Outstanding: 1, Pending: 1},
		DomainBacklogs: []statuspkg.DomainBacklog{
			{Domain: "workload_materialization", DeadLetter: 3, OldestAge: 12 * time.Minute},
			{Domain: "deployable_unit_correlation", DeadLetter: 1},
			{Domain: "cloud_asset_resolution", Outstanding: 1},
		},
		LatestQueueFailure: &statuspkg.QueueFailureSnapshot{
			Stage: "reducer", Domain: "workload_materialization", Status: "dead_letter",
			WorkItemID: "wi-secret-1", ScopeID: "scope-secret-1", GenerationID: "gen-secret-1",
			FailureClass: "merge_conflict", UpdatedAt: time.Date(2026, 6, 19, 2, 55, 0, 0, time.UTC),
		},
	})
	if got := operatorHealthState(t, payload); got != "degraded" {
		t.Fatalf("health state = %q, want degraded (queue dead letters)", got)
	}
	dead := payload["dead_letters"].(map[string]any)
	if got := dead["queue_dead_letter"].(float64); got != 4 {
		t.Fatalf("queue_dead_letter = %v, want 4", got)
	}
	byDomain := dead["by_domain"].([]any)
	if len(byDomain) != 2 {
		t.Fatalf("by_domain len = %d, want 2 (zero-deadletter domains excluded)", len(byDomain))
	}
	top := byDomain[0].(map[string]any)
	if top["domain"] != "workload_materialization" || top["dead_letter"].(float64) != 3 {
		t.Fatalf("by_domain[0] = %#v, want workload_materialization=3", top)
	}
	latest := dead["latest_failure"].(map[string]any)
	if latest["failure_class"] != "merge_conflict" {
		t.Fatalf("latest_failure.failure_class = %#v, want merge_conflict", latest["failure_class"])
	}
	// Shared (non-scoped) tokens see raw correlation IDs.
	if latest["scope_id"] != "scope-secret-1" || latest["generation_id"] != "gen-secret-1" {
		t.Fatalf("shared latest_failure missing correlation IDs: %#v", latest)
	}
}

// TestOperatorControlPlaneSurfacesDeadLetterBeyondDefaultDomainCap guards the
// accuracy fix: a dead-lettered domain with low outstanding work must still
// appear even when more than the default top-N higher-outstanding domains exist.
func TestOperatorControlPlaneSurfacesDeadLetterBeyondDefaultDomainCap(t *testing.T) {
	t.Parallel()
	domains := []statuspkg.DomainBacklog{
		{Domain: "domain_a", Outstanding: 90},
		{Domain: "domain_b", Outstanding: 80},
		{Domain: "domain_c", Outstanding: 70},
		{Domain: "domain_d", Outstanding: 60},
		{Domain: "domain_e", Outstanding: 50},
		{Domain: "domain_f", Outstanding: 40},
		// Low outstanding but dead-lettered: would be dropped by the default cap of 5.
		{Domain: "rare_dead_letter_domain", Outstanding: 0, DeadLetter: 2, OldestAge: 8 * time.Minute},
	}
	payload := operatorControlPlaneRequest(t, statuspkg.RawSnapshot{
		AsOf:           time.Date(2026, 6, 19, 3, 0, 0, 0, time.UTC),
		Queue:          statuspkg.QueueSnapshot{DeadLetter: 2},
		DomainBacklogs: domains,
	})
	dead := payload["dead_letters"].(map[string]any)
	byDomain := dead["by_domain"].([]any)
	found := false
	for _, row := range byDomain {
		if row.(map[string]any)["domain"] == "rare_dead_letter_domain" {
			found = true
		}
	}
	if !found {
		t.Fatalf("by_domain dropped a dead-lettered domain past the default cap: %#v", byDomain)
	}
}

func TestOperatorControlPlaneDegradedCollectorGeneration(t *testing.T) {
	t.Parallel()
	payload := operatorControlPlaneRequest(t, statuspkg.RawSnapshot{
		AsOf: time.Date(2026, 6, 19, 3, 0, 0, 0, time.UTC),
		CollectorGenerationDeadLetters: statuspkg.CollectorGenerationDeadLetterSnapshot{
			DeadLetter: 2, ReplayRequested: 1, OldestDeadLetterAge: 30 * time.Minute,
		},
	})
	if got := operatorHealthState(t, payload); got != "degraded" {
		t.Fatalf("health state = %q, want degraded (collector generation dead letters)", got)
	}
	dead := payload["dead_letters"].(map[string]any)
	gen := dead["collector_generation"].(map[string]any)
	if got := gen["dead_letter"].(float64); got != 2 {
		t.Fatalf("collector_generation.dead_letter = %v, want 2", got)
	}
}

func TestOperatorControlPlaneScopedRedactsCorrelationIDs(t *testing.T) {
	t.Parallel()
	const (
		secretWorkItem   = "wi-secret-scoped"
		secretScope      = "scope-secret-scoped"
		secretGeneration = "gen-secret-scoped"
	)
	handler := &StatusHandler{StatusReader: fakeStatusReader{snapshot: statuspkg.RawSnapshot{
		AsOf:  time.Date(2026, 6, 19, 3, 0, 0, 0, time.UTC),
		Queue: statuspkg.QueueSnapshot{DeadLetter: 2},
		DomainBacklogs: []statuspkg.DomainBacklog{
			{Domain: "workload_materialization", DeadLetter: 2, OldestAge: 5 * time.Minute},
		},
		LatestQueueFailure: &statuspkg.QueueFailureSnapshot{
			Stage: "reducer", Domain: "workload_materialization", Status: "dead_letter",
			WorkItemID: secretWorkItem, ScopeID: secretScope, GenerationID: secretGeneration,
			FailureClass: "merge_conflict", UpdatedAt: time.Date(2026, 6, 19, 2, 55, 0, 0, time.UTC),
		},
	}}}
	mux := http.NewServeMux()
	handler.Mount(mux)
	resolver := &fakeScopedTokenResolver{
		context: AuthContext{Mode: AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "workspace-a"},
		ok:      true,
	}
	authed := AuthMiddlewareWithScopedTokens("", resolver, mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/operator-control-plane", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	authed.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("scoped status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	body := rec.Body.String()
	for _, secret := range []string{secretWorkItem, secretScope, secretGeneration, "tenant-a", "workspace-a"} {
		if strings.Contains(body, secret) {
			t.Fatalf("scoped operator read model leaked %q: %s", secret, body)
		}
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload["scoped"] != true {
		t.Fatalf("scoped flag = %#v, want true", payload["scoped"])
	}
	// Aggregate counts and the failure class stay visible; only raw IDs hide.
	dead := payload["dead_letters"].(map[string]any)
	if got := dead["queue_dead_letter"].(float64); got != 2 {
		t.Fatalf("scoped queue_dead_letter = %v, want 2 (counts preserved)", got)
	}
	latest := dead["latest_failure"].(map[string]any)
	if latest["failure_class"] != "merge_conflict" {
		t.Fatalf("scoped latest_failure.failure_class = %#v, want merge_conflict", latest["failure_class"])
	}
	for _, hidden := range []string{"work_item_id", "scope_id", "generation_id"} {
		if _, leaked := latest[hidden]; leaked {
			t.Fatalf("scoped latest_failure exposed %q: %#v", hidden, latest)
		}
	}
}
