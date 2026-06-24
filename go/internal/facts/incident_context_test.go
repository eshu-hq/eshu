// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "testing"

func TestIncidentContextFactKinds(t *testing.T) {
	t.Parallel()

	got := IncidentContextFactKinds()
	want := []string{
		IncidentRecordFactKind,
		IncidentLifecycleEventFactKind,
		ChangeRecordFactKind,
	}
	if len(got) != len(want) {
		t.Fatalf("IncidentContextFactKinds len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("IncidentContextFactKinds[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	got[0] = "mutated"
	if IncidentContextFactKinds()[0] != IncidentRecordFactKind {
		t.Fatal("IncidentContextFactKinds returned mutable backing slice")
	}
}

func TestIncidentContextSchemaVersion(t *testing.T) {
	t.Parallel()

	for _, kind := range IncidentContextFactKinds() {
		version, ok := IncidentContextSchemaVersion(kind)
		if !ok {
			t.Fatalf("IncidentContextSchemaVersion(%q) ok = false", kind)
		}
		if version != IncidentContextSchemaVersionV1 {
			t.Fatalf("IncidentContextSchemaVersion(%q) = %q, want %q", kind, version, IncidentContextSchemaVersionV1)
		}
	}
	if version, ok := IncidentContextSchemaVersion("unknown"); ok || version != "" {
		t.Fatalf("unknown schema version = %q, %v; want empty, false", version, ok)
	}
}
