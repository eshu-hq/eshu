// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestHTTPClientCollectConfigEvidenceUsesBoundedServiceAndIntegrationEndpoints(t *testing.T) {
	t.Parallel()

	now := testObservedAt()
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "Token token=pd-token"; got != want {
			t.Fatalf("Authorization = %q, want %q", got, want)
		}
		requested = append(requested, r.URL.Path)
		switch r.URL.Path {
		case "/services":
			if got, want := r.URL.Query().Get("limit"), "2"; got != want {
				t.Fatalf("service limit = %q, want %q", got, want)
			}
			writeJSON(t, w, map[string]any{
				"services": []map[string]any{{
					"id":                "SVC1",
					"summary":           "checkout-api",
					"status":            "active",
					"alert_creation":    "create_alerts_and_incidents",
					"html_url":          "https://example.pagerduty.com/services/SVC1?token=secret",
					"updated_at":        now.Format(time.RFC3339),
					"escalation_policy": map[string]any{"id": "EP1", "summary": "platform escalation"},
					"teams":             []map[string]any{{"id": "TEAM1", "summary": "platform team"}},
				}},
			})
		case "/services/SVC1/integrations":
			if got, want := r.URL.Query().Get("limit"), "2"; got != want {
				t.Fatalf("integration limit = %q, want %q", got, want)
			}
			writeJSON(t, w, map[string]any{
				"integrations": []map[string]any{{
					"id":              "INT1",
					"summary":         "CloudWatch alerts",
					"type":            "events_api_v2_inbound_integration",
					"integration_key": "routing-key-secret",
					"html_url":        "https://example.pagerduty.com/services/SVC1/integrations/INT1?routing_key=secret",
					"vendor":          map[string]any{"id": "PVENDOR", "summary": "Amazon CloudWatch"},
				}},
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{
		BaseURL: server.URL,
		Token:   "pd-token",
		Client:  server.Client(),
	})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	result, err := client.CollectConfigEvidence(context.Background(), TargetConfig{
		ConfigValidationEnabled: true,
		ConfigResourceLimit:     2,
	})
	if err != nil {
		t.Fatalf("CollectConfigEvidence() error = %v, want nil", err)
	}
	if got, want := len(result.Services), 1; got != want {
		t.Fatalf("len(Services) = %d, want %d", got, want)
	}
	if got, want := result.Services[0].ID, "SVC1"; got != want {
		t.Fatalf("service id = %q, want %q", got, want)
	}
	if got, want := len(result.Integrations), 1; got != want {
		t.Fatalf("len(Integrations) = %d, want %d", got, want)
	}
	if got, want := result.Integrations[0].ServiceID, "SVC1"; got != want {
		t.Fatalf("integration service id = %q, want %q", got, want)
	}
	if result.Integrations[0].RoutingKey != "" {
		t.Fatalf("integration RoutingKey = %q, want redacted empty value", result.Integrations[0].RoutingKey)
	}
	if result.Redactions == 0 {
		t.Fatal("Redactions = 0, want routing key redaction counted")
	}
	wantPaths := []string{"/services", "/services/SVC1/integrations"}
	for i, want := range wantPaths {
		if requested[i] != want {
			t.Fatalf("requested[%d] = %q, want %q; all paths %#v", i, requested[i], want, requested)
		}
	}
}

func TestHTTPClientCollectConfigEvidenceFollowsPaginatedServicePages(t *testing.T) {
	t.Parallel()

	var serviceOffsets []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/services":
			serviceOffsets = append(serviceOffsets, r.URL.Query().Get("offset"))
			if len(serviceOffsets) == 1 {
				writeJSON(t, w, map[string]any{
					"more": true,
					"services": []map[string]any{
						{"id": "SVC1", "status": "active"},
						{"id": "SVC2", "status": "active"},
					},
				})
				return
			}
			writeJSON(t, w, map[string]any{
				"more":     false,
				"services": []map[string]any{{"id": "SVC3", "status": "active"}},
			})
		case "/services/SVC1/integrations", "/services/SVC2/integrations", "/services/SVC3/integrations":
			writeJSON(t, w, map[string]any{"integrations": []map[string]any{}})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "pd-token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	result, err := client.CollectConfigEvidence(context.Background(), TargetConfig{
		ConfigValidationEnabled: true,
		ConfigResourceLimit:     2,
	})
	if err != nil {
		t.Fatalf("CollectConfigEvidence() error = %v, want nil", err)
	}
	if got, want := len(result.Services), 3; got != want {
		t.Fatalf("len(Services) = %d, want %d (previously the collector stopped at page 1)", got, want)
	}
	if got, want := len(serviceOffsets), 2; got != want {
		t.Fatalf("service page requests = %d, want %d", got, want)
	}
	if got, want := serviceOffsets[1], "2"; got != want {
		t.Fatalf("second page offset = %q, want %q", got, want)
	}
	if result.Truncated {
		t.Fatal("Truncated = true, want false: pagination exhausted more naturally")
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("Warnings = %#v, want none", result.Warnings)
	}
}

func TestHTTPClientCollectConfigEvidenceSetsTruncatedWhenServicePageBoundHit(t *testing.T) {
	t.Parallel()

	pageCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/services":
			pageCalls++
			id := "SVC" + strconv.Itoa(pageCalls)
			writeJSON(t, w, map[string]any{
				"more":     true,
				"services": []map[string]any{{"id": id, "status": "active"}},
			})
		case "/services/SVC1/integrations", "/services/SVC2/integrations":
			writeJSON(t, w, map[string]any{"integrations": []map[string]any{}})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "pd-token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	result, err := client.CollectConfigEvidence(context.Background(), TargetConfig{
		ConfigValidationEnabled: true,
		ConfigResourceLimit:     1,
		PaginationMaxPages:      2,
	})
	if err != nil {
		t.Fatalf("CollectConfigEvidence() error = %v, want nil", err)
	}
	if got, want := pageCalls, 2; got != want {
		t.Fatalf("service page requests = %d, want exactly %d (bounded)", got, want)
	}
	if got, want := len(result.Services), 2; got != want {
		t.Fatalf("len(Services) = %d, want %d", got, want)
	}
	if !result.Truncated {
		t.Fatal("Truncated = false, want true when the max-page bound is hit while more pages remain")
	}
	var truncationWarning *ConfigWarning
	for i := range result.Warnings {
		if result.Warnings[i].Reason == ConfigWarningTruncated {
			truncationWarning = &result.Warnings[i]
		}
	}
	if truncationWarning == nil {
		t.Fatalf("Warnings = %#v, want a %q warning", result.Warnings, ConfigWarningTruncated)
	}
	if got, want := truncationWarning.ResourceClass, ConfigResourceClassService; got != want {
		t.Fatalf("warning ResourceClass = %q, want %q", got, want)
	}
}

func TestHTTPClientCollectConfigEvidenceFollowsPaginatedIntegrationPages(t *testing.T) {
	t.Parallel()

	var integrationOffsets []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/services":
			writeJSON(t, w, map[string]any{
				"more":     false,
				"services": []map[string]any{{"id": "SVC1", "status": "active"}},
			})
		case "/services/SVC1/integrations":
			integrationOffsets = append(integrationOffsets, r.URL.Query().Get("offset"))
			if len(integrationOffsets) == 1 {
				writeJSON(t, w, map[string]any{
					"more":         true,
					"integrations": []map[string]any{{"id": "INT1", "type": "events_api_v2_inbound_integration"}},
				})
				return
			}
			writeJSON(t, w, map[string]any{
				"more":         false,
				"integrations": []map[string]any{{"id": "INT2", "type": "events_api_v2_inbound_integration"}},
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "pd-token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	result, err := client.CollectConfigEvidence(context.Background(), TargetConfig{
		ConfigValidationEnabled: true,
		ConfigResourceLimit:     10,
	})
	if err != nil {
		t.Fatalf("CollectConfigEvidence() error = %v, want nil", err)
	}
	if got, want := len(result.Integrations), 2; got != want {
		t.Fatalf("len(Integrations) = %d, want %d (previously the collector stopped at page 1)", got, want)
	}
	if got, want := len(integrationOffsets), 2; got != want {
		t.Fatalf("integration page requests = %d, want %d", got, want)
	}
}

func TestHTTPClientCollectConfigEvidenceUsesAllowedServiceIDsAndReportsMissingServices(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/services/SVC1":
			writeJSON(t, w, map[string]any{
				"service": map[string]any{"id": "SVC1", "summary": "checkout-api", "status": "active"},
			})
		case "/services/SVC1/integrations":
			writeJSON(t, w, map[string]any{"integrations": []map[string]any{}})
		case "/services/SVC2":
			http.Error(w, "not found", http.StatusNotFound)
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "pd-token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	result, err := client.CollectConfigEvidence(context.Background(), TargetConfig{
		ConfigValidationEnabled: true,
		ConfigResourceLimit:     10,
		AllowedServiceIDs:       []string{"SVC1", "SVC2"},
	})
	if err != nil {
		t.Fatalf("CollectConfigEvidence() error = %v, want nil", err)
	}
	if got, want := result.Services[0].MatchState, ConfigMatchStateNotCompared; got != want {
		t.Fatalf("service MatchState = %q, want %q", got, want)
	}
	if !result.Partial {
		t.Fatal("Partial = false, want true for missing allowlisted service")
	}
	if got, want := len(result.Warnings), 1; got != want {
		t.Fatalf("len(Warnings) = %d, want %d", got, want)
	}
	if got, want := result.Warnings[0].Reason, ConfigWarningMissing; got != want {
		t.Fatalf("warning reason = %q, want %q", got, want)
	}
}

func TestHTTPClientCollectConfigEvidenceKeepsPermissionHiddenIntegrationPartial(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/services":
			writeJSON(t, w, map[string]any{
				"services": []map[string]any{{"id": "SVC1", "summary": "checkout-api", "status": "active"}},
			})
		case "/services/SVC1/integrations":
			http.Error(w, "forbidden", http.StatusForbidden)
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "pd-token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	result, err := client.CollectConfigEvidence(context.Background(), TargetConfig{
		ConfigValidationEnabled: true,
		ConfigResourceLimit:     10,
	})
	if err != nil {
		t.Fatalf("CollectConfigEvidence() error = %v, want nil", err)
	}
	if got, want := len(result.Services), 1; got != want {
		t.Fatalf("len(Services) = %d, want %d", got, want)
	}
	if !result.Partial {
		t.Fatal("Partial = false, want true")
	}
	if got, want := result.Warnings[0].Reason, ConfigWarningPermissionHidden; got != want {
		t.Fatalf("warning reason = %q, want %q", got, want)
	}
}

func TestHTTPClientCollectConfigEvidenceReturnsRetryableIntegrationFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/services":
			writeJSON(t, w, map[string]any{
				"services": []map[string]any{{"id": "SVC1", "summary": "checkout-api", "status": "active"}},
			})
		case "/services/SVC1/integrations":
			http.Error(w, "temporary failure", http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "pd-token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	_, err = client.CollectConfigEvidence(context.Background(), TargetConfig{
		ConfigValidationEnabled: true,
		ConfigResourceLimit:     10,
	})
	if err == nil {
		t.Fatal("CollectConfigEvidence() error = nil, want retryable integration failure")
	}
	var pdErr PagerDutyError
	if !errors.As(err, &pdErr) {
		t.Fatalf("CollectConfigEvidence() error = %T, want PagerDutyError", err)
	}
	if got, want := pdErr.StatusCode, http.StatusInternalServerError; got != want {
		t.Fatalf("StatusCode = %d, want %d", got, want)
	}
}

func TestHTTPClientCollectConfigEvidenceMarksDeletedServices(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/services":
			writeJSON(t, w, map[string]any{
				"services": []map[string]any{{
					"id":      "SVC1",
					"summary": "retired checkout-api",
					"status":  "deleted",
				}},
			})
		case "/services/SVC1/integrations":
			writeJSON(t, w, map[string]any{"integrations": []map[string]any{}})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "pd-token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	result, err := client.CollectConfigEvidence(context.Background(), TargetConfig{
		ConfigValidationEnabled: true,
		ConfigResourceLimit:     10,
	})
	if err != nil {
		t.Fatalf("CollectConfigEvidence() error = %v, want nil", err)
	}
	if got, want := len(result.Services), 1; got != want {
		t.Fatalf("len(Services) = %d, want %d", got, want)
	}
	if !result.Services[0].Deleted {
		t.Fatalf("Services[0].Deleted = false, want true for deleted provider status")
	}
}

func TestHTTPClientCollectConfigEvidenceReturnsRateLimitForTopLevelConfigFetch(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "pd-token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	_, err = client.CollectConfigEvidence(context.Background(), TargetConfig{
		ConfigValidationEnabled: true,
		ConfigResourceLimit:     10,
	})
	if err == nil {
		t.Fatal("CollectConfigEvidence() error = nil, want rate-limit error")
	}
	var pdErr PagerDutyError
	if !errors.As(err, &pdErr) {
		t.Fatalf("CollectConfigEvidence() error = %T, want PagerDutyError", err)
	}
	if got, want := pdErr.StatusCode, http.StatusTooManyRequests; got != want {
		t.Fatalf("StatusCode = %d, want %d", got, want)
	}
	if strings.Contains(err.Error(), "pd-token") {
		t.Fatalf("CollectConfigEvidence() error = %q, want token redacted", err)
	}
}
