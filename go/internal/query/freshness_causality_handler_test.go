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

func freshnessCausalityRequest(t *testing.T, snapshot statuspkg.RawSnapshot) map[string]any {
	t.Helper()
	handler := &StatusHandler{StatusReader: fakeStatusReader{snapshot: snapshot}}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/freshness-causality", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; body=%s", err, rec.Body.String())
	}
	return payload
}

func TestFreshnessCausalityHandlerFresh(t *testing.T) {
	t.Parallel()
	payload := freshnessCausalityRequest(t, statuspkg.RawSnapshot{
		AsOf:              time.Date(2026, 6, 19, 3, 0, 0, 0, time.UTC),
		GenerationCounts:  []statuspkg.NamedCount{{Name: "active", Count: 4}},
		GenerationHistory: statuspkg.GenerationHistorySnapshot{Active: 4},
	})
	if payload["state"] != "fresh" {
		t.Fatalf("state = %v, want fresh", payload["state"])
	}
	causes := payload["causes"].([]any)
	if len(causes) != 7 {
		t.Fatalf("causes len = %d, want 7", len(causes))
	}
}

func TestFreshnessCausalityHandlerStaleAndRetracted(t *testing.T) {
	t.Parallel()
	payload := freshnessCausalityRequest(t, statuspkg.RawSnapshot{
		AsOf:              time.Date(2026, 6, 19, 3, 0, 0, 0, time.UTC),
		GenerationCounts:  []statuspkg.NamedCount{{Name: "active", Count: 2}, {Name: "superseded", Count: 3}},
		GenerationHistory: statuspkg.GenerationHistorySnapshot{Active: 2, Superseded: 3},
		DomainBacklogs: []statuspkg.DomainBacklog{
			{Domain: "workload_materialization", DeadLetter: 2, OldestAge: 9 * time.Minute},
		},
		GenerationTransitions: []statuspkg.GenerationTransitionSnapshot{
			{ScopeID: "scope-1", GenerationID: "gen-old", Status: "superseded", SupersededAt: time.Date(2026, 6, 19, 2, 0, 0, 0, time.UTC)},
		},
	})
	if payload["state"] != "stale" {
		t.Fatalf("state = %v, want stale", payload["state"])
	}
	gens := payload["generations"].(map[string]any)
	if gens["superseded"].(float64) != 3 {
		t.Fatalf("retired generations = %v, want 3", gens["superseded"])
	}
	transitions := payload["recent_transitions"].([]any)
	if len(transitions) != 1 {
		t.Fatalf("recent_transitions len = %d, want 1", len(transitions))
	}
}

func TestFreshnessCausalityHandlerScopedRedactsTransitionIDs(t *testing.T) {
	t.Parallel()
	const (
		secretScope = "scope-secret-fresh"
		secretGen   = "gen-secret-fresh"
	)
	handler := &StatusHandler{StatusReader: fakeStatusReader{snapshot: statuspkg.RawSnapshot{
		AsOf:              time.Date(2026, 6, 19, 3, 0, 0, 0, time.UTC),
		GenerationCounts:  []statuspkg.NamedCount{{Name: "active", Count: 1}, {Name: "superseded", Count: 1}},
		GenerationHistory: statuspkg.GenerationHistorySnapshot{Active: 1, Superseded: 1},
		GenerationTransitions: []statuspkg.GenerationTransitionSnapshot{
			{ScopeID: secretScope, GenerationID: secretGen, Status: "superseded", SupersededAt: time.Date(2026, 6, 19, 2, 0, 0, 0, time.UTC)},
		},
	}}}
	mux := http.NewServeMux()
	handler.Mount(mux)
	resolver := &fakeScopedTokenResolver{
		context: AuthContext{Mode: AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "workspace-a"},
		ok:      true,
	}
	authed := AuthMiddlewareWithScopedTokens("", resolver, mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/freshness-causality", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	authed.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("scoped status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, secret := range []string{secretScope, secretGen, "tenant-a", "workspace-a"} {
		if strings.Contains(body, secret) {
			t.Fatalf("scoped freshness causality leaked %q: %s", secret, body)
		}
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["scoped"] != true {
		t.Fatalf("scoped flag = %v, want true", payload["scoped"])
	}
	// The lifecycle status stays visible; only raw IDs are withheld.
	transitions := payload["recent_transitions"].([]any)
	if len(transitions) != 1 {
		t.Fatalf("recent_transitions len = %d, want 1", len(transitions))
	}
	row := transitions[0].(map[string]any)
	if row["status"] != "superseded" {
		t.Fatalf("transition status = %v, want superseded", row["status"])
	}
	for _, hidden := range []string{"scope_id", "generation_id"} {
		if _, leaked := row[hidden]; leaked {
			t.Fatalf("scoped transition exposed %q: %#v", hidden, row)
		}
	}
}
