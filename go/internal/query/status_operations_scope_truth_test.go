// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func TestGetOperationsScopedCallerWithholdsGlobalAggregatesAndDowngradesTruth(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	globalSnapshot := statuspkg.RawSnapshot{
		AsOf: asOf,
		Queue: statuspkg.QueueSnapshot{
			Total:       4242,
			Outstanding: 3131,
			InFlight:    2121,
		},
		StageCounts: []statuspkg.StageStatusCount{{Stage: "other_tenant_stage", Status: "pending", Count: 1111}},
		DomainBacklogs: []statuspkg.DomainBacklog{{
			Domain:      "other_tenant_domain",
			Outstanding: 1010,
		}},
		Coordinator: &statuspkg.CoordinatorSnapshot{
			CollectorInstances: []statuspkg.CollectorInstanceSummary{{
				InstanceID:     "other-tenant-collector",
				CollectorKind:  "other_tenant_collector",
				Enabled:        true,
				LastObservedAt: asOf,
				UpdatedAt:      asOf,
			}},
		},
	}
	reader := &fakeLiveActivityReader{rows: []statuspkg.LiveActivityRow{
		liveActivityTestRow("tenant-a-work-item", "repository:tenant-a", "tenant-a/repo", "tenant-a-worker", asOf),
	}}
	handler := &StatusHandler{
		StatusReader: fakeStatusReader{snapshot: globalSnapshot},
		LiveActivity: reader,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	authed := AuthMiddlewareWithScopedTokens("", &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:                 AuthModeScoped,
			TenantID:             "tenant-a",
			WorkspaceID:          "workspace-a",
			AllowedRepositoryIDs: []string{"repo-a"},
		},
		ok: true,
	}, mux)

	request := func(accept string) map[string]any {
		req := httptest.NewRequest(http.MethodGet, "/api/v0/status/operations", nil)
		req.Header.Set("Authorization", "Bearer tenant-a-token")
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		rec := httptest.NewRecorder()
		authed.ServeHTTP(rec, req)
		if got, want := rec.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("json.Unmarshal() error = %v; body = %s", err, rec.Body.String())
		}
		return payload
	}

	envelope := request(EnvelopeMIMEType)
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %#v, want scoped operations object", envelope["data"])
	}
	for _, key := range []string{"health", "collectors", "stage_summaries", "domain_backlogs", "queue"} {
		if _, disclosed := data[key]; disclosed {
			t.Errorf("scoped data disclosed global %q aggregate: %#v", key, data[key])
		}
	}
	encodedData, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("json.Marshal(data) error = %v", err)
	}
	for _, otherTenantValue := range []string{"other-tenant-collector", "other_tenant_collector", "other_tenant_stage", "other_tenant_domain"} {
		if strings.Contains(string(encodedData), otherTenantValue) {
			t.Errorf("scoped data disclosed other-tenant value %q: %s", otherTenantValue, encodedData)
		}
	}
	if got, want := data["completeness_state"], "scoped_live_activity_only"; got != want {
		t.Errorf("completeness_state = %#v, want %#v", got, want)
	}
	wantWithheld := []any{"health", "collectors", "stage_summaries", "domain_backlogs", "queue"}
	if got := data["withheld_sections"]; !reflect.DeepEqual(got, wantWithheld) {
		t.Errorf("withheld_sections = %#v, want %#v", got, wantWithheld)
	}
	truth, ok := envelope["truth"].(map[string]any)
	if !ok {
		t.Fatalf("truth = %#v, want object", envelope["truth"])
	}
	if got, want := truth["level"], string(TruthLevelDerived); got != want {
		t.Errorf("truth.level = %#v, want %#v", got, want)
	}
	if reason, _ := truth["reason"].(string); !strings.Contains(reason, "process-global aggregate sections are withheld") {
		t.Errorf("truth.reason = %#v, want explicit aggregate-withholding reason", truth["reason"])
	}

	legacy := request("application/json")
	if _, wrapped := legacy["data"]; wrapped {
		t.Fatalf("legacy scoped payload unexpectedly wrapped: %#v", legacy)
	}
	if !reflect.DeepEqual(legacy, data) {
		t.Fatalf("legacy scoped payload differs from envelope data:\nlegacy = %#v\ndata = %#v", legacy, data)
	}
}

func TestOpenAPIOperationsDocumentsScopedCompletenessBoundary(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}
	paths := spec["paths"].(map[string]any)
	operation := paths["/api/v0/status/operations"].(map[string]any)["get"].(map[string]any)
	description := operation["description"].(string)
	for _, fragment := range []string{
		"scoped responses omit those five sections",
		"completeness_state=scoped_live_activity_only",
		"carry derived truth",
		"All-scopes callers retain the complete aggregate board and exact truth",
	} {
		if !strings.Contains(description, fragment) {
			t.Errorf("operations description missing %q: %s", fragment, description)
		}
	}
	responses := operation["responses"].(map[string]any)
	content := responses["200"].(map[string]any)["content"].(map[string]any)
	schema := content["application/json"].(map[string]any)["schema"].(map[string]any)
	properties := schema["properties"].(map[string]any)
	for _, field := range []string{"completeness_state", "withheld_sections"} {
		if _, ok := properties[field]; !ok {
			t.Errorf("operations response schema missing %q", field)
		}
	}
}
