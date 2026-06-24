// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "testing"

func TestServiceCatalogFactKindRegistry(t *testing.T) {
	t.Parallel()

	wantKinds := []string{
		ServiceCatalogEntityFactKind,
		ServiceCatalogOwnershipFactKind,
		ServiceCatalogRepositoryLinkFactKind,
		ServiceCatalogDependencyFactKind,
		ServiceCatalogAPILinkFactKind,
		ServiceCatalogOperationalLinkFactKind,
		ServiceCatalogScorecardDefinitionFactKind,
		ServiceCatalogScorecardResultFactKind,
		ServiceCatalogWarningFactKind,
	}
	gotKinds := ServiceCatalogFactKinds()
	if len(gotKinds) != len(wantKinds) {
		t.Fatalf("ServiceCatalogFactKinds() len = %d, want %d: %#v", len(gotKinds), len(wantKinds), gotKinds)
	}
	for i, want := range wantKinds {
		if gotKinds[i] != want {
			t.Fatalf("ServiceCatalogFactKinds()[%d] = %q, want %q", i, gotKinds[i], want)
		}
		version, ok := ServiceCatalogSchemaVersion(want)
		if !ok {
			t.Fatalf("ServiceCatalogSchemaVersion(%q) ok = false, want true", want)
		}
		if version != "1.0.0" {
			t.Fatalf("ServiceCatalogSchemaVersion(%q) = %q, want 1.0.0", want, version)
		}
	}

	gotKinds[0] = "mutated"
	freshKinds := ServiceCatalogFactKinds()
	if freshKinds[0] != ServiceCatalogEntityFactKind {
		t.Fatalf("ServiceCatalogFactKinds() returned mutable backing slice: %#v", freshKinds)
	}
}
