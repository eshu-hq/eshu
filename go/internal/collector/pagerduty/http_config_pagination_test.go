// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

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

func TestHTTPClientCollectConfigEvidenceEnforcesMaxRecordsExactlyOnNonMultiplePageSize(t *testing.T) {
	t.Parallel()

	// End-to-end P2-1 proof through the wired collector: page size 2 with
	// pagination_max_records=3 is not an exact multiple. The final page must
	// request only the remaining 1 (no over-fetch) and the observed output
	// must be exactly 3 services, with Truncated set because more remained.
	var serviceLimits []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/services":
			serviceLimits = append(serviceLimits, r.URL.Query().Get("limit"))
			// Honor the requested limit (page size 2 otherwise); always more.
			pageSize := 2
			if raw := r.URL.Query().Get("limit"); raw != "" {
				if n, convErr := strconv.Atoi(raw); convErr == nil && n < pageSize {
					pageSize = n
				}
			}
			services := make([]map[string]any, 0, pageSize)
			base := len(serviceLimits) * 10
			for i := 0; i < pageSize; i++ {
				services = append(services, map[string]any{"id": "SVC" + strconv.Itoa(base+i), "status": "active"})
			}
			writeJSON(t, w, map[string]any{"more": true, "services": services})
		default:
			if strings.HasSuffix(r.URL.Path, "/integrations") {
				writeJSON(t, w, map[string]any{"integrations": []map[string]any{}})
				return
			}
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
		PaginationMaxRecords:    3,
	})
	if err != nil {
		t.Fatalf("CollectConfigEvidence() error = %v, want nil", err)
	}
	if got, want := len(result.Services), 3; got != want {
		t.Fatalf("len(Services) = %d, want exactly %d (non-multiple record cap enforced on output)", got, want)
	}
	if !result.Truncated {
		t.Fatal("Truncated = false, want true: more pages remained past the record cap")
	}
	wantLimits := []string{"2", "1"}
	if len(serviceLimits) != len(wantLimits) {
		t.Fatalf("service page limit params = %v, want %v (final page capped to the remaining allowance)", serviceLimits, wantLimits)
	}
	for i, want := range wantLimits {
		if serviceLimits[i] != want {
			t.Fatalf("service page[%d] limit = %q, want %q (final page must not over-fetch)", i, serviceLimits[i], want)
		}
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
