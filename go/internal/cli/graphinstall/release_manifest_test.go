// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphinstall

import "testing"

func TestEmbeddedNornicDBReleaseManifestHasNoBareAssetsWhileTrackingMain(t *testing.T) {
	manifest, err := readPinnedReleaseManifest()
	if err != nil {
		t.Fatalf("readPinnedReleaseManifest() error = %v, want nil", err)
	}
	if len(manifest.Releases) != 0 {
		t.Fatalf("embedded NornicDB releases = %d, want 0 while Eshu tracks latest NornicDB main via --from installs", len(manifest.Releases))
	}
}
