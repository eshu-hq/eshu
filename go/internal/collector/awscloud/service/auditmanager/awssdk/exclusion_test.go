// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsEvidenceAndMutation is the metadata-only acceptance
// gate the issue calls out for Audit Manager: the SDK adapter must never read
// collected evidence, evidence finder records, change logs, delegation comments,
// control narratives, or assessment report URLs, and must never mutate Audit
// Manager state. We reflect over the adapter's read interface and confirm no
// evidence read, report-URL read, change-log read, delegation read, control
// narrative read, or mutation method is reachable. This test fails the build if
// a future edit ever adds one of these to the adapter surface.
func TestAdapterInterfaceForbidsEvidenceAndMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		// evidence / report-url / change-log / delegation reads — never reachable.
		"Evidence", "ReportUrl", "ChangeLogs", "Delegation",
		// GetControl returns the control narrative (testing information, action
		// plan instructions); the scanner uses ListControls metadata only.
		"GetControl",
		// insights aggregate compliance posture, not control-plane metadata.
		"Insights",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Start", "Stop",
		"Register", "Deregister", "Associate", "Disassociate",
		"Batch", "Tag", "Untag", "Validate", "Assign",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the Audit Manager read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden evidence/narrative/mutation method %q; the Audit Manager adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the Audit Manager adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreListOrGetReads asserts every method on the adapter
// interface is a List or Get read so the read surface stays explicit and
// auditable. The scanner reads assessment, framework, and control metadata, the
// account-level settings KMS key, and resource tags only; nothing fetches
// evidence, report URLs, or control narrative bodies.
func TestAdapterMethodsAreListOrGetReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Get") {
			t.Fatalf("apiClient method %q is not a List or Get read", name)
		}
	}
}
