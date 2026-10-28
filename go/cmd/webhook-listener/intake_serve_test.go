// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/webhook/intake"
)

// serveTestStores returns coalescing fakes backing every intake family so
// the served-intake tests exercise the production mount path end to end.
func serveTestStores() (intake.Stores, *parityRepositoryStore, *parityIncidentStore, *parityAWSStore, *parityGCPStore, *parityActionsStore) {
	repo := &parityRepositoryStore{}
	incident := &parityIncidentStore{}
	aws := &parityAWSStore{}
	gcp := &parityGCPStore{}
	actions := &parityActionsStore{}
	stores := intake.Stores{
		Repository: repo,
		Incident:   incident,
		AWS:        aws,
		GCP:        gcp,
		Actions:    actions,
	}
	return stores, repo, incident, aws, gcp, actions
}

func serveTestConfig() webhookListenerConfig {
	return webhookListenerConfig{
		IntakeToken:         "serve-token",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
		GitHubSecret:        "secret",
		GitHubPath:          "/webhooks/github",
		PagerDutySecret:     "secret",
		PagerDutyPath:       "/webhooks/pagerduty",
		PagerDutyScopeID:    "pagerduty:account:example",
		DefaultBranch:       "main",
	}
}

// mountServedMux builds the production served-intake server for cfg and
// mounts it on a fresh mux, failing the test when the mount is refused.
func mountServedMux(t *testing.T, cfg webhookListenerConfig, stores intake.Stores) *http.ServeMux {
	t.Helper()
	server, err := buildServedIntake(cfg, stores, nil)
	if err != nil {
		t.Fatalf("buildServedIntake() error = %v, want nil", err)
	}
	if server == nil {
		t.Fatalf("buildServedIntake() = nil, want serving server")
	}
	mux := http.NewServeMux()
	mountServedIntake(mux, server)
	return mux
}

func postServedIntake(mux *http.ServeMux, route string, body []byte, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, route, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decodeServedBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode served intake body %q: %v", rec.Body.String(), err)
	}
	return decoded
}

func validRepositoryIntakeBody() []byte {
	return []byte(`{
		"api_version": "v1",
		"provider": "github",
		"delivery_id": "serve-delivery-1",
		"event_kind": "push",
		"decision": "accepted",
		"repository_external_id": "42",
		"repository_full_name": "eshu-hq/eshu",
		"default_branch": "main",
		"ref": "refs/heads/main",
		"target_sha": "2222222222222222222222222222222222222222"
	}`)
}

func TestServeIntakeDisabledWithoutToken(t *testing.T) {
	t.Parallel()

	stores, _, _, _, _, _ := serveTestStores()
	cfg := serveTestConfig()
	cfg.IntakeToken = ""
	server, err := buildServedIntake(cfg, stores, nil)
	if err != nil {
		t.Fatalf("buildServedIntake() error = %v, want nil", err)
	}
	if server != nil {
		t.Fatalf("buildServedIntake() without token = %v, want nil (fail closed)", server)
	}
	mux := http.NewServeMux()
	mountServedIntake(mux, server)
	rec := postServedIntake(mux, "/api/v1/webhook/intake/repository", validRepositoryIntakeBody(), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unmounted intake route status = %d, want 404", rec.Code)
	}
}

func TestServeIntakeDisabledInIntakeMode(t *testing.T) {
	t.Parallel()

	stores, _, _, _, _, _ := serveTestStores()
	cfg := serveTestConfig()
	cfg.IntakeURL = "https://core.example.test"
	server, err := buildServedIntake(cfg, stores, nil)
	if err != nil {
		t.Fatalf("buildServedIntake() error = %v, want nil", err)
	}
	if server != nil {
		t.Fatalf("buildServedIntake() in intake mode = %v, want nil (submitter keeps sole authority, no loopback serve)", server)
	}
}

func TestServedIntakeAcceptsValidRepositoryDelivery(t *testing.T) {
	t.Parallel()

	stores, repo, _, _, _, _ := serveTestStores()
	mux := mountServedMux(t, serveTestConfig(), stores)
	rec := postServedIntake(mux, "/api/v1/webhook/intake/repository", validRepositoryIntakeBody(), "serve-token")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("valid repository intake status = %d, want 202 body=%q", rec.Code, rec.Body.String())
	}
	decoded := decodeServedBody(t, rec)
	if decoded["api_version"] != intake.APIVersionV1 {
		t.Fatalf("api_version = %v, want %q", decoded["api_version"], intake.APIVersionV1)
	}
	triggerID, _ := decoded["trigger_id"].(string)
	if triggerID == "" {
		t.Fatalf("trigger_id missing in 202 receipt body=%q", rec.Body.String())
	}
	if got := repo.rowCount(); got != 1 {
		t.Fatalf("repository rows = %d, want 1", got)
	}
}

func TestServedIntakeDuplicateDeliveryCoalesces(t *testing.T) {
	t.Parallel()

	stores, repo, _, _, _, _ := serveTestStores()
	mux := mountServedMux(t, serveTestConfig(), stores)
	for i := 0; i < 2; i++ {
		rec := postServedIntake(mux, "/api/v1/webhook/intake/repository", validRepositoryIntakeBody(), "serve-token")
		if rec.Code != http.StatusAccepted {
			t.Fatalf("delivery %d status = %d, want 202", i, rec.Code)
		}
		if i == 1 {
			decoded := decodeServedBody(t, rec)
			if decoded["duplicate"] != true {
				t.Fatalf("redelivery receipt duplicate = %v, want true body=%q", decoded["duplicate"], rec.Body.String())
			}
		}
	}
	if got := repo.rowCount(); got != 1 {
		t.Fatalf("repository rows after redelivery = %d, want 1", got)
	}
}

func validActionsIntakeBody(deliveryID, runID string) []byte {
	return []byte(`{
		"api_version": "v1",
		"provider": "github",
		"delivery_id": "` + deliveryID + `",
		"event_kind": "workflow_run",
		"action": "completed",
		"repository_id": "42",
		"repository": "example/repo",
		"run_id": "` + runID + `"
	}`)
}

func TestServedIntakeActionsConflictReturns409(t *testing.T) {
	t.Parallel()

	stores, _, _, _, _, actions := serveTestStores()
	mux := mountServedMux(t, serveTestConfig(), stores)
	for i := 0; i < 2; i++ {
		rec := postServedIntake(mux, "/api/v1/webhook/intake/actions", validActionsIntakeBody("actions-delivery-1", "123"), "serve-token")
		if rec.Code != http.StatusAccepted {
			t.Fatalf("exact replay %d status = %d, want 202 body=%q", i, rec.Code, rec.Body.String())
		}
	}
	rec := postServedIntake(mux, "/api/v1/webhook/intake/actions", validActionsIntakeBody("actions-delivery-1", "999"), "serve-token")
	if rec.Code != http.StatusConflict {
		t.Fatalf("conflicting identity status = %d, want 409 body=%q", rec.Code, rec.Body.String())
	}
	if decoded := decodeServedBody(t, rec); decoded["code"] != "delivery_conflict" {
		t.Fatalf("conflict code = %v, want delivery_conflict", decoded["code"])
	}
	if got := actions.rowCount(); got != 1 {
		t.Fatalf("actions rows after conflict = %d, want 1 (original intact)", got)
	}
}

func TestServedIntakeRejectsBadBearer(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		token string
	}{
		{name: "missing", token: ""},
		{name: "wrong", token: "wrong-token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stores, repo, _, _, _, _ := serveTestStores()
			mux := mountServedMux(t, serveTestConfig(), stores)
			rec := postServedIntake(mux, "/api/v1/webhook/intake/repository", validRepositoryIntakeBody(), tc.token)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("bad Bearer %s status = %d, want 401", tc.name, rec.Code)
			}
			if got := repo.rowCount(); got != 0 {
				t.Fatalf("repository rows after 401 = %d, want 0", got)
			}
		})
	}
}

func TestServedIntakeEnforcesBounds(t *testing.T) {
	t.Parallel()

	stores, _, _, _, _, _ := serveTestStores()
	cfg := serveTestConfig()
	cfg.MaxRequestBodyBytes = 64
	mux := mountServedMux(t, cfg, stores)
	oversized := []byte(`{"api_version":"v1","padding":"` + strings.Repeat("x", 128) + `"}`)
	if rec := postServedIntake(mux, "/api/v1/webhook/intake/repository", oversized, "serve-token"); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body status = %d, want 413", rec.Code)
	}

	stores, repo, _, _, _, _ := serveTestStores()
	mux = mountServedMux(t, serveTestConfig(), stores)
	cases := []struct {
		name string
		body string
		code string
	}{
		{name: "invalid_json", body: `{"api_version":`, code: ""},
		{name: "unsupported_api_version", body: `{"api_version":"v9"}`, code: "unsupported_api_version"},
		{name: "unsupported_provider", body: `{"api_version":"v1","provider":"ado"}`, code: "unsupported_provider"},
		{name: "invalid_trigger", body: `{"api_version":"v1","provider":"github"}`, code: "invalid_trigger"},
	}
	for _, tc := range cases {
		rec := postServedIntake(mux, "/api/v1/webhook/intake/repository", []byte(tc.body), "serve-token")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400 body=%q", tc.name, rec.Code, rec.Body.String())
		}
		if tc.code != "" {
			if decoded := decodeServedBody(t, rec); decoded["code"] != tc.code {
				t.Fatalf("%s: code = %v, want %q", tc.name, decoded["code"], tc.code)
			}
		}
	}
	if got := repo.rowCount(); got != 0 {
		t.Fatalf("repository rows after bound violations = %d, want 0", got)
	}
}

func TestServedIntakeUnconfiguredFamilyReturns503(t *testing.T) {
	t.Parallel()

	repo := &parityRepositoryStore{}
	stores := intake.Stores{Repository: repo}
	mux := mountServedMux(t, serveTestConfig(), stores)
	incidentBody := []byte(`{
		"api_version": "v1",
		"provider": "pagerduty",
		"event_kind": "incident.triggered",
		"event_id": "evt-1",
		"scope_id": "pagerduty:account:example"
	}`)
	rec := postServedIntake(mux, "/api/v1/webhook/intake/incident", incidentBody, "serve-token")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured incident family status = %d, want 503 body=%q", rec.Code, rec.Body.String())
	}
	if decoded := decodeServedBody(t, rec); decoded["code"] != "store_failed" {
		t.Fatalf("unconfigured family code = %v, want store_failed", decoded["code"])
	}
}

func TestServedIntakeSharesAdminProbes(t *testing.T) {
	t.Parallel()

	stores, _, _, _, _, _ := serveTestStores()
	appMux := mountServedMux(t, serveTestConfig(), stores)
	adminMux, err := runtimecfg.NewStatusAdminMux("webhook-listener", &fakeStatusReader{}, appMux, runtimecfg.WithPrometheusHandler(http.NotFoundHandler()))
	if err != nil {
		t.Fatalf("NewStatusAdminMux() error = %v, want nil", err)
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		rec := httptest.NewRecorder()
		adminMux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200 body=%q", path, rec.Code, rec.Body.String())
		}
	}
	rec := postServedIntake(adminMux, "/api/v1/webhook/intake/repository", validRepositoryIntakeBody(), "serve-token")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("intake through admin mux status = %d, want 202 body=%q", rec.Code, rec.Body.String())
	}
}

// The provider-edge divergences (#9404) never reach the intake API on either
// path: the shared handler answers them before any store or submitter call,
// so the extracted path matches direct by construction. These cases pin that.
func TestExtractedPathPingIsAcceptedWithoutStoring(t *testing.T) {
	t.Parallel()

	world := newParityWorld(t, "listener-token")
	payload := []byte(`{"zen":"keep it simple"}`)
	rec := world.post("/webhooks/github", payload, map[string]string{
		"X-GitHub-Event":      "ping",
		"X-GitHub-Delivery":   "parity-ping-1",
		"X-Hub-Signature-256": githubSignature("secret", payload),
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("ping status = %d, want 202 body=%q", rec.Code, rec.Body.String())
	}
	if got := world.repo.rowCount() + world.actions.rowCount(); got != 0 {
		t.Fatalf("stored rows after ping = %d, want 0", got)
	}
}

func TestExtractedPathUnsupportedEventRejected(t *testing.T) {
	t.Parallel()

	world := newParityWorld(t, "listener-token")
	payload := []byte(`{"action":"opened"}`)
	rec := world.post("/webhooks/github", payload, map[string]string{
		"X-GitHub-Event":      "issues",
		"X-GitHub-Delivery":   "parity-unsupported-1",
		"X-Hub-Signature-256": githubSignature("secret", payload),
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unsupported event status = %d, want 400 body=%q", rec.Code, rec.Body.String())
	}
	if got := world.repo.rowCount() + world.actions.rowCount(); got != 0 {
		t.Fatalf("stored rows after unsupported event = %d, want 0", got)
	}
}

func TestExtractedPathMissingDeliveryRejected(t *testing.T) {
	t.Parallel()

	world := newParityWorld(t, "listener-token")
	payload := []byte(`{
		"ref":"refs/heads/main",
		"before":"1111111111111111111111111111111111111111",
		"after":"2222222222222222222222222222222222222222",
		"repository":{"id":42,"full_name":"eshu-hq/eshu","default_branch":"main"}
	}`)
	rec := world.post("/webhooks/github", payload, map[string]string{
		"X-GitHub-Event":      "push",
		"X-Hub-Signature-256": githubSignature("secret", payload),
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing delivery status = %d, want 400 body=%q", rec.Code, rec.Body.String())
	}
	if got := world.repo.rowCount(); got != 0 {
		t.Fatalf("stored rows after missing delivery = %d, want 0", got)
	}
}
