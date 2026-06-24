// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "testing"

func TestWorkItemFactKinds(t *testing.T) {
	t.Parallel()

	want := []string{
		WorkItemRecordFactKind,
		WorkItemTransitionFactKind,
		WorkItemExternalLinkFactKind,
		WorkItemProjectMetadataFactKind,
		WorkItemIssueTypeMetadataFactKind,
		WorkItemStatusMetadataFactKind,
		WorkItemWorkflowMetadataFactKind,
		WorkItemFieldMetadataFactKind,
		WorkItemMetadataWarningFactKind,
	}
	got := WorkItemFactKinds()
	if len(got) != len(want) {
		t.Fatalf("WorkItemFactKinds len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("WorkItemFactKinds[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	got[0] = "mutated"
	if WorkItemFactKinds()[0] != WorkItemRecordFactKind {
		t.Fatal("WorkItemFactKinds returned mutable backing slice")
	}
}

func TestWorkItemSchemaVersion(t *testing.T) {
	t.Parallel()

	for _, kind := range WorkItemFactKinds() {
		version, ok := WorkItemSchemaVersion(kind)
		if !ok {
			t.Fatalf("WorkItemSchemaVersion(%q) ok = false", kind)
		}
		if version != WorkItemSchemaVersionV1 {
			t.Fatalf("WorkItemSchemaVersion(%q) = %q, want %q", kind, version, WorkItemSchemaVersionV1)
		}
	}
	if version, ok := WorkItemSchemaVersion("unknown"); ok || version != "" {
		t.Fatalf("WorkItemSchemaVersion(unknown) = %q, %v; want empty false", version, ok)
	}
}
