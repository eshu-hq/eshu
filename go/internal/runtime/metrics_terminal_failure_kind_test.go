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

// TestStatusMetricsHandlerTerminalFailureGaugeUsesPerKindClasses pins that the
// gauge's failure_class label carries each kind's own bounded class (#8896)
// and stays closed: a class only another kind owns reads unknown, and every
// free-form class an extension collector defines shares one extension_defined
// series instead of minting a label value per class.
func TestStatusMetricsHandlerTerminalFailureGaugeUsesPerKindClasses(t *testing.T) {
	handler, err := NewStatusMetricsHandler("workflow-coordinator", &fakeStatusReader{
		snapshot: statuspkg.RawSnapshot{
			AsOf: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC),
			Coordinator: &statuspkg.CoordinatorSnapshot{
				CollectorInstances: []collector.InstanceSummary{
					{InstanceID: "jira-a", CollectorKind: "jira", Mode: "continuous", Enabled: true, ClaimsEnabled: true},
					{InstanceID: "jira-b", CollectorKind: "jira", Mode: "continuous", Enabled: true, ClaimsEnabled: true},
					{InstanceID: "aws-a", CollectorKind: "aws", Mode: "continuous", Enabled: true, ClaimsEnabled: true},
					{InstanceID: "ext-a", CollectorKind: "vendor_kind", Mode: "continuous", Enabled: true, ClaimsEnabled: true},
					{InstanceID: "ext-b", CollectorKind: "vendor_kind", Mode: "continuous", Enabled: true, ClaimsEnabled: true},
				},
				CollectorTerminalFailures: []collector.TerminalFailure{
					{CollectorKind: "jira", InstanceID: "jira-a", FailureClass: "permission_hidden", FailureOperation: "search_issues"},
					{CollectorKind: "jira", InstanceID: "jira-b", FailureClass: "stale_fence", FailureOperation: "search_issues"},
					{CollectorKind: "aws", InstanceID: "aws-a", FailureClass: "stale_fence", FailureOperation: "collect_failure"},
					{CollectorKind: "vendor_kind", InstanceID: "ext-a", FailureClass: "vendor.quota-exceeded"},
					{CollectorKind: "vendor_kind", InstanceID: "ext-b", FailureClass: "vendor.other-failure"},
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

	const series = `eshu_runtime_coordinator_collector_terminal_failure_instances`
	for _, want := range []string{
		series + `{collector_kind="jira",failure_class="permission_hidden",service_name="workflow-coordinator"} 1`,
		series + `{collector_kind="jira",failure_class="unknown",service_name="workflow-coordinator"} 1`,
		series + `{collector_kind="aws",failure_class="stale_fence",service_name="workflow-coordinator"} 1`,
		series + `{collector_kind="vendor_kind",failure_class="extension_defined",service_name="workflow-coordinator"} 2`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("GET /metrics body missing %q\nbody:\n%s", want, body)
		}
	}
	if strings.Contains(body, "vendor.quota-exceeded") || strings.Contains(body, "vendor.other-failure") {
		t.Fatalf("GET /metrics leaked an extension-defined class as a label value:\n%s", body)
	}
}
