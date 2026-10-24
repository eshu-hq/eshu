// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

// TestStatusMetricsHandlerSplitsAcknowledgedTerminalFailures pins the gauge
// split for operator acknowledgements (#8830): an acknowledged instance leaves
// the terminal_failure_instances series, which alerts page on (an instance with both kinds counts in both), and appears in
// terminal_failure_acknowledged_instances, so a failure nobody fixed stays
// countable without paging.
func TestStatusMetricsHandlerSplitsAcknowledgedTerminalFailures(t *testing.T) {
	handler, err := NewStatusMetricsHandler("workflow-coordinator", &fakeStatusReader{
		snapshot: statuspkg.RawSnapshot{
			AsOf: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC),
			Coordinator: &statuspkg.CoordinatorSnapshot{
				CollectorInstances: []collector.InstanceSummary{
					{InstanceID: "alerts-open", CollectorKind: "security_alert", Mode: "continuous", Enabled: true, ClaimsEnabled: true},
					{InstanceID: "alerts-acked-a", CollectorKind: "security_alert", Mode: "continuous", Enabled: true, ClaimsEnabled: true},
					{InstanceID: "alerts-acked-b", CollectorKind: "security_alert", Mode: "continuous", Enabled: true, ClaimsEnabled: true},
					{InstanceID: "alerts-both", CollectorKind: "security_alert", Mode: "continuous", Enabled: true, ClaimsEnabled: true},
				},
				CollectorTerminalFailures: []collector.TerminalFailure{
					{CollectorKind: "security_alert", InstanceID: "alerts-open", FailureClass: "auth_denied", FailureOperation: "list_org_alerts"},
					{CollectorKind: "security_alert", InstanceID: "alerts-acked-a", FailureClass: "auth_denied", FailureOperation: "list_org_alerts", Acknowledged: true},
					{CollectorKind: "security_alert", InstanceID: "alerts-acked-b", FailureClass: "not_found", FailureOperation: "list_org_alerts", Acknowledged: true},
					{CollectorKind: "security_alert", InstanceID: "alerts-both", FailureClass: "rate_limited", FailureOperation: "list_org_alerts"},
					{CollectorKind: "security_alert", InstanceID: "alerts-both", FailureClass: "auth_denied", FailureOperation: "list_org_alerts", Acknowledged: true},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewStatusMetricsHandler() error = %v, want nil", err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := recorder.Body.String()

	for _, want := range []string{
		`eshu_runtime_coordinator_collector_terminal_failure_instances{collector_kind="security_alert",failure_class="auth_denied",service_name="workflow-coordinator"} 1`,
		`eshu_runtime_coordinator_collector_terminal_failure_instances{collector_kind="security_alert",failure_class="rate_limited",service_name="workflow-coordinator"} 1`,
		`eshu_runtime_coordinator_collector_terminal_failure_acknowledged_instances{collector_kind="security_alert",failure_class="auth_denied",service_name="workflow-coordinator"} 2`,
		`eshu_runtime_coordinator_collector_terminal_failure_acknowledged_instances{collector_kind="security_alert",failure_class="not_found",service_name="workflow-coordinator"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("GET /metrics body missing %q\nbody:\n%s", want, body)
		}
	}
	if strings.Contains(body, `terminal_failure_instances{collector_kind="security_alert",failure_class="not_found"`) {
		t.Fatalf("an acknowledged failure is still counted as an open terminal failure:\n%s", body)
	}
}
