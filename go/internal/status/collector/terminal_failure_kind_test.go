// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector_test

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

// kindClassRow runs one terminal failure through RuntimeStatuses for an
// enabled, coordinator-registered instance of kind and returns the row.
func kindClassRow(t *testing.T, kind string, class string) collector.RuntimeStatus {
	t.Helper()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	evidence := collector.Evidence{
		AsOf: now,
		Instances: []collector.InstanceSummary{{
			InstanceID:     "kind-example",
			CollectorKind:  kind,
			Mode:           "continuous",
			Enabled:        true,
			ClaimsEnabled:  true,
			LastObservedAt: now.Add(-time.Minute),
			UpdatedAt:      now.Add(-time.Minute),
		}},
		TerminalFailures: []collector.TerminalFailure{{
			CollectorKind:    kind,
			InstanceID:       "kind-example",
			FailureClass:     class,
			FailureOperation: "collect_failure",
			ObservedAt:       now.Add(-time.Minute),
		}},
	}
	for _, candidate := range collector.RuntimeStatuses(evidence) {
		if candidate.InstanceID == "kind-example" {
			return candidate
		}
	}
	t.Fatalf("no runtime row for kind %q", kind)
	return collector.RuntimeStatus{}
}

// TestRuntimeStatusesOtherKindTerminalClassesReadTheirOwnClass pins that the
// bounded classes each non-security-alert collector kind persists on a
// terminal work item reach the operator as that class, not as unknown (#8896).
// Each case names the writer so a rename in the collector shows up here.
func TestRuntimeStatusesOtherKindTerminalClassesReadTheirOwnClass(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind   string
		class  string
		writer string
	}{
		{"jira", "permission_hidden", "sdk.FailurePermissionHidden via jiraStatusPolicy (401, 403)"},
		{"jira", "deleted", "sdk.FailureDeleted via jiraStatusPolicy (404)"},
		{"jira", "archived", "sdk.FailureArchived via jiraStatusPolicy (410)"},
		{"aws", "permission_denied", "awsruntime.FailureClassPermissionDenied"},
		{"aws", "unsupported_permission", "awsruntime.FailureClassUnsupportedPermission"},
		{"aws", "stale_fence", "awsruntime.FailureClassStaleFence"},
		{"oci_registry", "registry_auth_denied", "collector.RegistryFailureAuthDenied"},
		{"oci_registry", "registry_not_found", "collector.RegistryFailureNotFound"},
		{"oci_registry", "registry_rate_limited", "collector.RegistryFailureRateLimited after a spent attempt budget"},
		{"oci_registry", "registry_retryable_failure", "collector.RegistryFailureRetryable after a spent attempt budget"},
		{"oci_registry", "registry_canceled", "collector.RegistryFailureCanceled"},
		{"oci_registry", "registry_terminal_failure", "collector.RegistryFailureTerminal"},
		{"package_registry", "registry_auth_denied", "collector.RegistryFailureAuthDenied"},
		{"package_registry", "registry_terminal_failure", "collector.RegistryFailureTerminal"},
		{"package_registry", "registry_metadata_too_large", "package runtime metadataTooLargeError"},
		{"sbom_attestation", "registry_not_found", "sbom runtime RegistryHTTPFailure"},
		{"vulnerability_intelligence", "non_retryable", "vulnruntime sourceFailure on a 4xx"},
		{"scanner_worker", "timeout", "scannercomponent.FailureClassTimeout"},
		{"scanner_worker", "analyzer_failed", "scannercomponent.FailureClassAnalyzerFailed"},
		{"terraform_state", "waiting_on_git_generation", "terraformstate.WaitingOnGitGenerationError"},
		{"custom_vendor_kind", "launch_failure", "extensionhost.FailureClassLaunchFailure"},
		{"custom_vendor_kind", "invalid_result", "extensionhost.FailureClassInvalidResult"},
	}
	for _, tc := range cases {
		t.Run(tc.kind+"/"+tc.class, func(t *testing.T) {
			t.Parallel()
			row := kindClassRow(t, tc.kind, tc.class)
			if row.Health != "degraded" || row.LastFailureClass != tc.class {
				t.Fatalf("%s row = health %q class %q, want degraded with %q (writer: %s)",
					tc.kind, row.Health, row.LastFailureClass, tc.class, tc.writer)
			}
			want := "terminal fetch failure: " + tc.class + " (collect_failure)"
			if row.Detail != want {
				t.Fatalf("Detail = %q, want %q", row.Detail, want)
			}
		})
	}
}

// TestRuntimeStatusesKindClassesStayClosed pins that the per-kind sets never
// let free text through: a class outside the kind's own set reads unknown,
// even when another kind owns it, and an extension kind's own free-form class
// collapses to one bounded token rather than a growing enum.
func TestRuntimeStatusesKindClassesStayClosed(t *testing.T) {
	t.Parallel()
	tooLong := "a" + strings.Repeat("b", 96)
	cases := []struct {
		name  string
		kind  string
		class string
		want  string
	}{
		{"another kind's class stays unknown", "jira", "permission_denied", "unknown"},
		{"registry class on a git kind stays unknown", "git", "registry_auth_denied", "unknown"},
		{"first-party kind never takes the extension token", "jira", "vendor_specific_failure", "unknown"},
		{"metadata too large belongs to package_registry only", "oci_registry", "registry_metadata_too_large", "unknown"},
		{"free text on a first-party kind", "aws", "access denied for arn:aws:iam::1:role/x", "unknown"},
		{"empty class", "aws", "", "unknown"},
		{"extension kind pattern-valid third-party class", "custom_vendor_kind", "vendor.quota-exceeded", "extension_defined"},
		{"extension kind does not leak a first-party class", "custom_vendor_kind", "permission_denied", "extension_defined"},
		{"extension kind free text", "custom_vendor_kind", "bad token: s3cr3t value", "unknown"},
		{"extension kind uppercase", "custom_vendor_kind", "Vendor_Failure", "unknown"},
		{"extension kind over the 96 byte pattern", "custom_vendor_kind", tooLong, "unknown"},
		{"extension kind keeps a common class", "custom_vendor_kind", "terminal", "terminal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			row := kindClassRow(t, tc.kind, tc.class)
			if row.LastFailureClass != tc.want {
				t.Fatalf("%s class %q read %q, want %q", tc.kind, tc.class, row.LastFailureClass, tc.want)
			}
			if strings.Contains(row.Detail, "s3cr3t") || strings.Contains(row.Detail, "arn:aws") {
				t.Fatalf("Detail = %q, want no persisted free text", row.Detail)
			}
		})
	}
}
