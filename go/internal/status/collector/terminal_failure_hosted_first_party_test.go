// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

// hostFailureClasses are the classes the extension host writes for a hosted
// kind (extensionhost/failure.go) that are not already common.
var hostFailureClasses = []string{"invalid_claim", "invalid_result", "launch_failure", "status_record"}

// TestFirstPartyKindHostedByExtensionHostReadsHostClasses pins #8896 review
// finding P2-1. The component-extension runtime takes its collector kind from
// the manifest (cmd/collector-component-extension collectorKindForManifest),
// and a component-configured instance can carry a first-party kind such as
// jira. The host writes its own classes on that instance, so every kind must
// read them as themselves instead of unknown.
func TestFirstPartyKindHostedByExtensionHostReadsHostClasses(t *testing.T) {
	t.Parallel()
	for _, kind := range scope.AllCollectorKinds() {
		for _, class := range hostFailureClasses {
			if got := collector.BoundedFailureClassForKind(string(kind), class); got != class {
				t.Errorf("BoundedFailureClassForKind(%q, %q) = %q, want the host class", kind, class, got)
			}
			if !slices.Contains(collector.FailureClassesForKind(string(kind)), class) {
				t.Errorf("FailureClassesForKind(%q) omits the host class %q", kind, class)
			}
			if row := kindClassRow(t, string(kind), class); row.LastFailureClass != class {
				t.Errorf("kind %q row class = %q, want %q", kind, row.LastFailureClass, class)
			}
		}
	}
}

// hostedKindClassRow is kindClassRow for an instance whose ExtensionHosted
// marker is set, the way the status reader fills it for a component-activated
// instance.
func hostedKindClassRow(t *testing.T, kind string, class string, hosted bool) collector.RuntimeStatus {
	t.Helper()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	evidence := collector.Evidence{
		AsOf: now,
		Instances: []collector.InstanceSummary{{
			InstanceID:      "hosted-example",
			CollectorKind:   kind,
			Mode:            "continuous",
			Enabled:         true,
			ClaimsEnabled:   true,
			ExtensionHosted: hosted,
			LastObservedAt:  now.Add(-time.Minute),
			UpdatedAt:       now.Add(-time.Minute),
		}},
		TerminalFailures: []collector.TerminalFailure{{
			CollectorKind:    kind,
			InstanceID:       "hosted-example",
			FailureClass:     class,
			FailureOperation: "collect_failure",
			ObservedAt:       now.Add(-time.Minute),
		}},
	}
	for _, candidate := range collector.RuntimeStatuses(evidence) {
		if candidate.InstanceID == "hosted-example" {
			return candidate
		}
	}
	t.Fatalf("no runtime row for kind %q", kind)
	return collector.RuntimeStatus{}
}

// TestExtensionHostedFirstPartyInstanceReadsExtensionDefined pins #8951. The
// extension host takes its collector kind from the manifest, so a
// component-activated instance can carry a first-party kind such as jira. A
// class its extension defines itself reads extension_defined on that instance,
// like it does for any non-first-party kind, and an in-tree instance of the
// same kind keeps reading unknown so a class an in-tree writer forgot to
// register is never relabeled.
func TestExtensionHostedFirstPartyInstanceReadsExtensionDefined(t *testing.T) {
	t.Parallel()
	for _, kind := range scope.AllCollectorKinds() {
		hosted := hostedKindClassRow(t, string(kind), "vendor_quota_exceeded", true)
		if hosted.LastFailureClass != collector.ExtensionDefinedFailureToken {
			t.Errorf("extension-hosted %q read %q, want %q", kind, hosted.LastFailureClass, collector.ExtensionDefinedFailureToken)
		}
		inTree := hostedKindClassRow(t, string(kind), "vendor_quota_exceeded", false)
		if inTree.LastFailureClass != collector.UnknownFailureToken {
			t.Errorf("in-tree %q read %q, want %q", kind, inTree.LastFailureClass, collector.UnknownFailureToken)
		}
		if got := collector.BoundedFailureClassForKind(string(kind), "vendor_quota_exceeded"); got != collector.UnknownFailureToken {
			t.Errorf("BoundedFailureClassForKind(%q) = %q, want unknown for the kind-only read", kind, got)
		}
		if slices.Contains(collector.FailureClassesForKind(string(kind)), collector.ExtensionDefinedFailureToken) {
			t.Errorf("FailureClassesForKind(%q) must not report %q: the kind alone is not extension-hosted", kind, collector.ExtensionDefinedFailureToken)
		}
	}
}

// TestExtensionHostedFirstPartyInstanceStaysBounded pins that the hosted
// signal widens only the pattern-valid extension-defined classes. The kind's
// own and common classes read as themselves, and free text, an empty class,
// and an over-long class still read unknown, so the gauge label set stays
// closed.
func TestExtensionHostedFirstPartyInstanceStaysBounded(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		class string
		want  string
	}{
		{"own class", "permission_hidden", "permission_hidden"},
		{"common class", "terminal", "terminal"},
		{"host class", "launch_failure", "launch_failure"},
		{"extension-defined class", "vendor.quota-exceeded", "extension_defined"},
		{"free text", "bad token: s3cr3t value", "unknown"},
		{"uppercase", "Vendor_Failure", "unknown"},
		{"over the 96 byte pattern", "a" + strings.Repeat("b", 96), "unknown"},
		{"empty class", "", "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			row := hostedKindClassRow(t, "jira", tc.class, true)
			if row.LastFailureClass != tc.want {
				t.Fatalf("hosted jira class %q read %q, want %q", tc.class, row.LastFailureClass, tc.want)
			}
			if strings.Contains(row.Detail, "s3cr3t") {
				t.Fatalf("Detail = %q, want no persisted free text", row.Detail)
			}
		})
	}
}
