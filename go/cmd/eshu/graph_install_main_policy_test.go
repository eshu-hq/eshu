package main

import "testing"

func TestEmbeddedNornicDBReleaseManifestHasNoBareAssetsWhileTrackingMain(t *testing.T) {
	manifest, err := readPinnedNornicDBReleaseManifest()
	if err != nil {
		t.Fatalf("readPinnedNornicDBReleaseManifest() error = %v, want nil", err)
	}
	if len(manifest.Releases) != 0 {
		t.Fatalf("embedded NornicDB releases = %d, want 0 while Eshu tracks latest NornicDB main via --from installs", len(manifest.Releases))
	}
}
