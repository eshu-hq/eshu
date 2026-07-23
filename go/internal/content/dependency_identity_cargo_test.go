// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content

import "testing"

// cargoDependencyMetadata builds the entity_metadata a Cargo.toml manifest
// dependency row (rust/cargo_dependencies.go's cargoDependencyRow) contributes,
// with the given TOML manifest key (the local alias) and resolved crate name.
func cargoDependencyMetadata(section, manifestName string) map[string]any {
	return map[string]any{
		"config_kind":     "dependency",
		"package_manager": "cargo",
		"section":         section,
		"manifest_name":   manifestName,
	}
}

// TestCanonicalEntityIDWithMetadataCargoAdmitsInScopeRow proves an ordinary
// Cargo.toml manifest dependency row (#5507) routes to the section-keyed
// scheme, not the legacy line-keyed CanonicalEntityID.
func TestCanonicalEntityIDWithMetadataCargoAdmitsInScopeRow(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "Cargo.toml"
		name   = "serde"
		line   = 8
	)
	metadata := cargoDependencyMetadata("dependencies", "serde")

	got := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line, metadata)
	if legacy := CanonicalEntityID(repoID, path, "Variable", name, line); got == legacy {
		t.Fatalf("CanonicalEntityIDWithMetadata() = %q unexpectedly matched legacy CanonicalEntityID() for an in-scope cargo row", got)
	}
}

// TestCanonicalEntityIDWithMetadataCargoReorderNoChurn proves that a Cargo
// dependency's identity does not change when its source line moves — the
// same [dependencies] table redeclared in a different position in the file
// (e.g. after an unrelated dependency was added above it) mints the same id.
func TestCanonicalEntityIDWithMetadataCargoReorderNoChurn(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "Cargo.toml"
		name   = "serde"
	)
	metadata := cargoDependencyMetadata("dependencies", "serde")

	before := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 3, metadata)
	after := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 30, metadata)
	if before != after {
		t.Fatalf("reordering changed the cargo dependency id: line 3 = %q, line 30 = %q", before, after)
	}
}

// TestCanonicalEntityIDWithMetadataCargoAliasDistinctness proves the THE TRAP
// case #5507 flagged for cargo: a manifest can depend on the same crate twice
// under two different local aliases in one [dependencies] table (e.g.
// `tokio1 = { package = "tokio", version = "1" }` and
// `tokio02 = { package = "tokio", version = "0.2" }`) to bridge two major
// versions simultaneously. Both rows share the same resolved crate name
// ("tokio") and the same section, so (section, name) alone would collapse
// them into one node — the manifest_name discriminator must keep them
// distinct.
func TestCanonicalEntityIDWithMetadataCargoAliasDistinctness(t *testing.T) {
	t.Parallel()

	const (
		repoID       = "repository:r_12345678"
		path         = "Cargo.toml"
		resolvedName = "tokio"
	)

	tokio1 := CanonicalEntityIDWithMetadata(repoID, path, "Variable", resolvedName, 4,
		cargoDependencyMetadata("dependencies", "tokio1"))
	tokio02 := CanonicalEntityIDWithMetadata(repoID, path, "Variable", resolvedName, 5,
		cargoDependencyMetadata("dependencies", "tokio02"))

	if tokio1 == tokio02 {
		t.Fatalf("two distinct cargo aliases of the same crate collapsed into one id: %q", tokio1)
	}
}

// TestCanonicalEntityIDWithMetadataCargoCrossSectionDistinctness proves a
// dependency declared under [dependencies] stays distinct from the same name
// declared under [dev-dependencies].
func TestCanonicalEntityIDWithMetadataCargoCrossSectionDistinctness(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "Cargo.toml"
		name   = "serde"
		line   = 4
	)

	runtimeID := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line,
		cargoDependencyMetadata("dependencies", "serde"))
	devID := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line,
		cargoDependencyMetadata("dev-dependencies", "serde"))

	if runtimeID == devID {
		t.Fatalf("dependencies and dev-dependencies sections collapsed into one id: %q", runtimeID)
	}
}
