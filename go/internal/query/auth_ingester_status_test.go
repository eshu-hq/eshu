// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

func TestAuthMiddlewareWithScopedTokensAllowsIngesterStatusRoutes(t *testing.T) {
	t.Parallel()

	const (
		privateInstanceID  = "collector-private-tenant"
		privateDisplayName = "private tenant collector"
		privateConflictKey = "repo://private/repository"
	)
	now := time.Date(2026, 6, 10, 5, 0, 0, 0, time.UTC)
	statusHandler := &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: now,
				Coordinator: &statuspkg.CoordinatorSnapshot{
					CollectorInstances: []statuspkg.CollectorInstanceSummary{{
						InstanceID:     privateInstanceID,
						CollectorKind:  "git",
						Mode:           "continuous",
						DisplayName:    privateDisplayName,
						Enabled:        true,
						LastObservedAt: now,
						UpdatedAt:      now,
					}},
					ActiveClaims: 2,
				},
				QueueBlockages: []statuspkg.QueueBlockage{{
					Stage:       "reducer",
					Domain:      "repository_projection",
					ConflictKey: privateConflictKey,
					Blocked:     1,
				}},
			},
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	statusHandler.Mount(mux)
	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:        AuthModeScoped,
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-a",
		},
		ok: true,
	}
	handler := AuthMiddlewareWithScopedTokens("", resolver, mux)

	listReq := httptest.NewRequest(http.MethodGet, "/api/v0/status/ingesters", nil)
	listReq.Header.Set("Authorization", "Bearer scoped-token")
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if got, want := listRec.Code, http.StatusOK; got != want {
		t.Fatalf("list status = %d, want %d; body=%s", got, want, listRec.Body.String())
	}
	assertIngesterStatusResponseRedacted(t, listRec.Body.String(), privateInstanceID, privateDisplayName, privateConflictKey)

	detailReq := httptest.NewRequest(http.MethodGet, "/api/v0/status/ingesters/repository", nil)
	detailReq.Header.Set("Authorization", "Bearer scoped-token")
	detailRec := httptest.NewRecorder()
	handler.ServeHTTP(detailRec, detailReq)
	if got, want := detailRec.Code, http.StatusOK; got != want {
		t.Fatalf("detail status = %d, want %d; body=%s", got, want, detailRec.Body.String())
	}
	assertIngesterStatusResponseRedacted(t, detailRec.Body.String(), privateInstanceID, privateDisplayName, privateConflictKey)

	var detail map[string]any
	if err := json.Unmarshal(detailRec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("json.Unmarshal(detail) error = %v, want nil", err)
	}
	coordinator, ok := detail["coordinator"].(map[string]any)
	if !ok {
		t.Fatalf("detail.coordinator = %#v, want object", detail["coordinator"])
	}
	if _, leaked := coordinator["collector_instances"]; leaked {
		t.Fatalf("scoped coordinator exposed collector_instances: %#v", coordinator)
	}
	if got, want := coordinator["collector_instance_count"], float64(1); got != want {
		t.Fatalf("collector_instance_count = %#v, want %#v", got, want)
	}
}

func assertIngesterStatusResponseRedacted(t *testing.T, body string, forbiddenValues ...string) {
	t.Helper()

	forbidden := append([]string{
		"conflict_key",
		"tenant-a",
		"workspace-a",
	}, forbiddenValues...)
	for _, value := range forbidden {
		if value == "" {
			continue
		}
		if strings.Contains(body, value) {
			t.Fatalf("ingester status leaked %q: %s", value, body)
		}
	}
}
