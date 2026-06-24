// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scope

import "testing"

func TestAllCollectorKindsIsStableAndUnique(t *testing.T) {
	t.Parallel()

	kinds := AllCollectorKinds()
	if len(kinds) == 0 {
		t.Fatal("AllCollectorKinds returned no kinds")
	}

	seen := map[CollectorKind]bool{}
	for _, kind := range kinds {
		if kind == "" {
			t.Fatal("AllCollectorKinds returned an empty collector kind")
		}
		if seen[kind] {
			t.Fatalf("AllCollectorKinds returned duplicate kind %q", kind)
		}
		seen[kind] = true
	}

	for _, required := range []CollectorKind{CollectorGit, CollectorAWS, CollectorTerraformState, CollectorLoki} {
		if !seen[required] {
			t.Errorf("AllCollectorKinds missing required kind %q", required)
		}
	}
}
