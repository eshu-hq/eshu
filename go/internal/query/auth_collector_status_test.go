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

func TestAuthMiddlewareWithScopedTokensAllowsCollectorStatusRoute(t *testing.T) {
	t.Parallel()

	const (
		privateInstanceID  = "collector-private-tenant"
		privateDisplayName = "private tenant collector"
		privateSource      = "source-system-private"
		privateDetail      = "service=private-service region=private-region"
		privateConflictKey = "repo://private/repository"
	)
	now := time.Date(2026, 6, 10, 5, 30, 0, 0, time.UTC)
	statusHandler := &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: now,
				Coordinator: &statuspkg.CoordinatorSnapshot{
					CollectorInstances: []statuspkg.CollectorInstanceSummary{{
						InstanceID:     privateInstanceID,
						CollectorKind:  "aws",
						Mode:           "continuous",
						DisplayName:    privateDisplayName,
						Enabled:        true,
						ClaimsEnabled:  true,
						LastObservedAt: now,
						UpdatedAt:      now,
					}},
				},
				AWSCloudScans: []statuspkg.AWSCloudScanStatus{{
					CollectorInstanceID: privateInstanceID,
					Region:              "private-region",
					ServiceKind:         "private-service",
					Status:              "succeeded",
					CommitStatus:        "committed",
					LastObservedAt:      now,
					UpdatedAt:           now,
				}},
				CollectorFactEvidence: []statuspkg.CollectorFactEvidence{{
					InstanceID:       privateInstanceID,
					CollectorKind:    "aws",
					EvidenceSource:   "collector_fact",
					SourceSystems:    []string{privateSource},
					ObservationCount: 7,
					LastObservedAt:   now,
					UpdatedAt:        now,
				}},
				QueueBlockages: []statuspkg.QueueBlockage{{
					Stage:       "collector",
					Domain:      "hosted_collection",
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

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/collectors", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	assertCollectorStatusResponseRedacted(t, rec.Body.String(), privateInstanceID, privateDisplayName, privateSource, privateDetail, privateConflictKey)

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	collectors, ok := payload["collectors"].([]any)
	if !ok || len(collectors) != 1 {
		t.Fatalf("collectors = %#v, want one aggregate row", payload["collectors"])
	}
	row, ok := collectors[0].(map[string]any)
	if !ok {
		t.Fatalf("collectors[0] = %#v, want object", collectors[0])
	}
	if got, want := row["collector_kind"], "aws"; got != want {
		t.Fatalf("collector_kind = %#v, want %#v", got, want)
	}
	if got, want := row["collector_count"], float64(1); got != want {
		t.Fatalf("collector_count = %#v, want %#v", got, want)
	}
	for _, forbiddenField := range []string{"instance_id", "display_name", "source_systems", "detail"} {
		if _, leaked := row[forbiddenField]; leaked {
			t.Fatalf("scoped collector row exposed %q: %#v", forbiddenField, row)
		}
	}

	legacyReq := httptest.NewRequest(http.MethodGet, "/api/v0/collectors", nil)
	legacyReq.Header.Set("Authorization", "Bearer scoped-token")
	legacyRec := httptest.NewRecorder()
	handler.ServeHTTP(legacyRec, legacyReq)
	if got, want := legacyRec.Code, http.StatusForbidden; got != want {
		t.Fatalf("legacy status = %d, want %d; body=%s", got, want, legacyRec.Body.String())
	}
}

func assertCollectorStatusResponseRedacted(t *testing.T, body string, forbiddenValues ...string) {
	t.Helper()

	forbidden := append([]string{
		"instance_id",
		"display_name",
		"source_systems",
		"detail",
		"conflict_key",
		"tenant-a",
		"workspace-a",
	}, forbiddenValues...)
	for _, value := range forbidden {
		if value == "" {
			continue
		}
		if strings.Contains(body, value) {
			t.Fatalf("collector status leaked %q: %s", value, body)
		}
	}
}
