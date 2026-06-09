package facts

import (
	"slices"
	"testing"
)

func TestGCPFactKindRegistry(t *testing.T) {
	kinds := GCPFactKinds()
	want := []string{
		GCPCloudResourceFactKind,
		GCPCollectionWarningFactKind,
	}
	if len(kinds) != len(want) {
		t.Fatalf("len(GCPFactKinds()) = %d, want %d", len(kinds), len(want))
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("GCPFactKinds()[%d] = %q, want %q", i, kinds[i], want[i])
		}
		version, ok := GCPSchemaVersion(kinds[i])
		if !ok {
			t.Fatalf("GCPSchemaVersion(%q) ok = false", kinds[i])
		}
		if version != "1.0.0" {
			t.Fatalf("GCPSchemaVersion(%q) = %q, want 1.0.0", kinds[i], version)
		}
	}

	kinds[0] = "mutated"
	if got := GCPFactKinds()[0]; got != GCPCloudResourceFactKind {
		t.Fatalf("GCPFactKinds returned mutable backing slice, got first kind %q", got)
	}
}

func TestGCPSchemaVersionUnknownKind(t *testing.T) {
	if _, ok := GCPSchemaVersion("gcp_not_a_kind"); ok {
		t.Fatal("GCPSchemaVersion(unknown) ok = true, want false")
	}
}

func TestGCPFactKindsAreCore(t *testing.T) {
	core := CoreFactKinds()
	for _, kind := range GCPFactKinds() {
		if !slices.Contains(core, kind) {
			t.Fatalf("CoreFactKinds() missing GCP kind %q", kind)
		}
		if !IsCoreFactKind(kind) {
			t.Fatalf("IsCoreFactKind(%q) = false, want true", kind)
		}
	}
}
