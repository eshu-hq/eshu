// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import "testing"

// TestRegistryAdmitsHaskellCabalMetadataOnly pins the Others Haskell (vi)
// lane: exactly ".cabal" binds metadata-only, neighbors stay unadmitted.
func TestRegistryAdmitsHaskellCabalMetadataOnly(t *testing.T) {
	t.Parallel()
	registry := DefaultRegistry()
	definition, ok := registry.LookupByPath("services/api/demo.cabal")
	if !ok || definition.ParserKey != "haskell_cabal" || definition.Language != "cabal" ||
		definition.Ecosystem != "haskell" || definition.ContentStore.Policy != ContentStoreMetadata {
		t.Fatalf("demo.cabal = %+v %v, want metadata-only haskell_cabal/cabal/haskell", definition, ok)
	}
	for _, path := range []string{"cabal.project", "cabal.project.freeze", "stack.yaml.lock"} {
		if _, ok := registry.LookupByPath(path); ok {
			t.Fatalf("%q is admitted, want unadmitted", path)
		}
	}
	if hs, ok := registry.LookupByPath("Main.hs"); !ok || hs.Language != "haskell" {
		t.Fatalf("Main.hs = %+v %v, want the haskell source parser", hs, ok)
	}
}
