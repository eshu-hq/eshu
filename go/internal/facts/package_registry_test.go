package facts

import "testing"

func TestPackageRegistryFactKindRegistry(t *testing.T) {
	t.Parallel()

	wantKinds := []string{
		PackageRegistryPackageFactKind,
		PackageRegistryPackageVersionFactKind,
		PackageRegistryPackageDependencyFactKind,
		PackageRegistryPackageArtifactFactKind,
		PackageRegistrySourceHintFactKind,
		PackageRegistryVulnerabilityHintFactKind,
		PackageRegistryRegistryEventFactKind,
		PackageRegistryRepositoryHostingFactKind,
		PackageRegistryWarningFactKind,
	}

	gotKinds := PackageRegistryFactKinds()
	if len(gotKinds) != len(wantKinds) {
		t.Fatalf("PackageRegistryFactKinds() len = %d, want %d: %#v", len(gotKinds), len(wantKinds), gotKinds)
	}
	for i, want := range wantKinds {
		if gotKinds[i] != want {
			t.Fatalf("PackageRegistryFactKinds()[%d] = %q, want %q", i, gotKinds[i], want)
		}
		version, ok := PackageRegistrySchemaVersion(want)
		if !ok {
			t.Fatalf("PackageRegistrySchemaVersion(%q) ok = false, want true", want)
		}
		if version != "1.0.0" {
			t.Fatalf("PackageRegistrySchemaVersion(%q) = %q, want 1.0.0", want, version)
		}
	}

	gotKinds[0] = "mutated"
	freshKinds := PackageRegistryFactKinds()
	if freshKinds[0] != PackageRegistryPackageFactKind {
		t.Fatalf("PackageRegistryFactKinds() returned mutable backing slice: %#v", freshKinds)
	}
}
