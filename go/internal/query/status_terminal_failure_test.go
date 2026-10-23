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
	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

func terminalFailureStatusHandler(now time.Time, failures ...collector.TerminalFailure) *StatusHandler {
	return &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: now,
				Coordinator: &statuspkg.CoordinatorSnapshot{
					CollectorInstances: []collector.InstanceSummary{{
						InstanceID:     "security-alert-example",
						CollectorKind:  "security_alert",
						Mode:           "continuous",
						Enabled:        true,
						ClaimsEnabled:  true,
						LastObservedAt: now,
						UpdatedAt:      now,
					}},
					CollectorTerminalFailures: failures,
				},
			},
		},
	}
}

func getStatusJSON(t *testing.T, handler *StatusHandler, path string) (map[string]any, string) {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200: %s", path, rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return payload, rec.Body.String()
}

func TestListCollectorsExposesBoundedTerminalFailure(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	failedAt := now.Add(-time.Minute)
	handler := terminalFailureStatusHandler(now, collector.TerminalFailure{
		CollectorKind:    "security_alert",
		InstanceID:       "security-alert-example",
		FailureClass:     "auth_denied",
		FailureOperation: "list_org_alerts",
		Terminal:         true,
		ObservedAt:       failedAt,
	})

	payload, _ := getStatusJSON(t, handler, "/api/v0/status/collectors")
	data := payload
	rows := data["collectors"].([]any)
	row := rows[0].(map[string]any)
	if row["health"] != "degraded" {
		t.Fatalf("health = %v, want degraded", row["health"])
	}
	if row["last_failure_class"] != "auth_denied" || row["last_failure_operation"] != "list_org_alerts" ||
		row["last_failure_terminal"] != true || row["last_failure_at"] != failedAt.Format(time.RFC3339) {
		t.Fatalf("failure fields = %v, want auth_denied/list_org_alerts/true/%s", row, failedAt.Format(time.RFC3339))
	}
}

func TestListCollectorsOmitsFailureFieldsWhenHealthy(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	payload, _ := getStatusJSON(t, terminalFailureStatusHandler(now), "/api/v0/status/collectors")
	row := payload["collectors"].([]any)[0].(map[string]any)
	for _, key := range []string{"last_failure_class", "last_failure_operation", "last_failure_terminal", "last_failure_at"} {
		if _, ok := row[key]; ok {
			t.Fatalf("healthy row carries %q: %v", key, row)
		}
	}
}

func TestCollectorReadinessNamesTerminalFailureClass(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	handler := terminalFailureStatusHandler(now, collector.TerminalFailure{
		CollectorKind:    "security_alert",
		InstanceID:       "security-alert-example",
		FailureClass:     "auth_denied",
		FailureOperation: "list_org_alerts",
		Terminal:         true,
		ObservedAt:       now.Add(-time.Minute),
	})

	payload, body := getStatusJSON(t, handler, "/api/v0/status/collector-readiness")
	entry := findReadinessEntry(t, payload, "security-alert-example")
	if entry["promotion_state"] != collector.PromotionFailed {
		t.Fatalf("promotion_state = %v, want failed", entry["promotion_state"])
	}
	blockers := entry["blockers"].([]any)
	if len(blockers) != 1 || blockers[0] != "terminal provider failure: auth_denied (list_org_alerts)" {
		t.Fatalf("blockers = %v, want terminal provider failure blocker", blockers)
	}
	if entry["last_failure_class"] != "auth_denied" || entry["last_failure_operation"] != "list_org_alerts" {
		t.Fatalf("entry = %v, want failure class and operation", entry)
	}
	if strings.Contains(body, "failure_message") {
		t.Fatalf("readiness body mentions failure_message: %s", body)
	}
}

func TestCollectorReadinessTerminalFailureThenSuccessFlipsBack(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	// After a later completed work item the reader returns no failure row.
	payload, _ := getStatusJSON(t, terminalFailureStatusHandler(now), "/api/v0/status/collector-readiness")
	entry := findReadinessEntry(t, payload, "security-alert-example")
	if entry["promotion_state"] == collector.PromotionFailed {
		t.Fatalf("promotion_state = failed after recovery: %v", entry)
	}
	if _, ok := entry["last_failure_class"]; ok {
		t.Fatalf("recovered entry still carries last_failure_class: %v", entry)
	}
}

func findReadinessEntry(t *testing.T, data map[string]any, instanceID string) map[string]any {
	t.Helper()
	for _, raw := range data["readiness"].([]any) {
		entry := raw.(map[string]any)
		if entry["instance_id"] == instanceID {
			return entry
		}
	}
	t.Fatalf("no readiness entry for %q in %v", instanceID, data)
	return nil
}
