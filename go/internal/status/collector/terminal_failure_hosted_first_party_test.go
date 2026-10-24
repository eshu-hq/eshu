// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector_test

import (
	"slices"
	"testing"

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

// TestFirstPartyKindKeepsExtensionDefinedLimit pins the deliberate limit that
// goes with the host classes: a class an extension defines itself collapses to
// extension_defined only for a kind that is not first-party. The status leaf
// has no instance-level signal that a first-party kind is extension-hosted, so
// on such an instance the extension's own class reads unknown, and a class an
// in-tree writer forgot to register is never relabeled extension_defined.
func TestFirstPartyKindKeepsExtensionDefinedLimit(t *testing.T) {
	t.Parallel()
	for _, kind := range scope.AllCollectorKinds() {
		if got := collector.BoundedFailureClassForKind(string(kind), "vendor_quota_exceeded"); got != collector.UnknownFailureToken {
			t.Errorf("first-party kind %q read an extension-defined class as %q, want unknown", kind, got)
		}
		if slices.Contains(collector.FailureClassesForKind(string(kind)), collector.ExtensionDefinedFailureToken) {
			t.Errorf("first-party kind %q must not report %q", kind, collector.ExtensionDefinedFailureToken)
		}
	}
	if got := collector.BoundedFailureClassForKind("hosted_vendor", "vendor_quota_exceeded"); got != collector.ExtensionDefinedFailureToken {
		t.Errorf("hosted_vendor read %q, want %q", got, collector.ExtensionDefinedFailureToken)
	}
}
