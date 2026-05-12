package facts

import "testing"

func TestOCIRegistryFactKindRegistry(t *testing.T) {
	t.Parallel()

	wantKinds := []string{
		OCIRegistryRepositoryFactKind,
		OCIImageTagObservationFactKind,
		OCIImageManifestFactKind,
		OCIImageIndexFactKind,
		OCIImageDescriptorFactKind,
		OCIImageReferrerFactKind,
		OCIRegistryWarningFactKind,
	}

	gotKinds := OCIRegistryFactKinds()
	if len(gotKinds) != len(wantKinds) {
		t.Fatalf("OCIRegistryFactKinds() len = %d, want %d: %#v", len(gotKinds), len(wantKinds), gotKinds)
	}
	for i, want := range wantKinds {
		if gotKinds[i] != want {
			t.Fatalf("OCIRegistryFactKinds()[%d] = %q, want %q", i, gotKinds[i], want)
		}
		version, ok := OCIRegistrySchemaVersion(want)
		if !ok {
			t.Fatalf("OCIRegistrySchemaVersion(%q) ok = false, want true", want)
		}
		if version != "1.0.0" {
			t.Fatalf("OCIRegistrySchemaVersion(%q) = %q, want 1.0.0", want, version)
		}
	}

	gotKinds[0] = "mutated"
	freshKinds := OCIRegistryFactKinds()
	if freshKinds[0] != OCIRegistryRepositoryFactKind {
		t.Fatalf("OCIRegistryFactKinds() returned mutable backing slice: %#v", freshKinds)
	}
}
