// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content

import "testing"

// sbtDependencyMetadata builds the entity_metadata an sbt build.sbt
// dependency row (sbt/rows.go's row) contributes: config_kind is always
// "dependency", package_manager and lang are "sbt", section is
// "libraryDependencies" with ":<Config>" appended when the declaration names
// a configuration, value is the literal revision, and sbt_cross_version is
// one of "none" (%), "binary" (%%), "scalajs" (%%%) or "custom".
func sbtDependencyMetadata(section, value, cross string) map[string]any {
	return map[string]any{
		"config_kind":       "dependency",
		"package_manager":   "sbt",
		"lang":              "sbt",
		"section":           section,
		"value":             value,
		"sbt_cross_version": cross,
	}
}

// TestCanonicalEntityIDWithMetadataSbtAdmitsInScopeRow proves an ordinary
// sbt dependency row (#8962) routes to the section-keyed scheme.
func TestCanonicalEntityIDWithMetadataSbtAdmitsInScopeRow(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "build.sbt"
		name   = "org.typelevel:cats-core"
		line   = 3
	)
	metadata := sbtDependencyMetadata("libraryDependencies", "2.10.0", "binary")

	got := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line, metadata)
	if legacy := CanonicalEntityID(repoID, path, "Variable", name, line); got == legacy {
		t.Fatalf("CanonicalEntityIDWithMetadata() = %q unexpectedly matched legacy CanonicalEntityID() for an in-scope sbt row", got)
	}
}

// TestCanonicalEntityIDWithMetadataSbtReorderNoChurn proves an sbt
// dependency's identity is stable when its declaration moves within the file.
func TestCanonicalEntityIDWithMetadataSbtReorderNoChurn(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "build.sbt"
		name   = "org.typelevel:cats-core"
	)
	metadata := sbtDependencyMetadata("libraryDependencies", "2.10.0", "binary")

	before := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 5, metadata)
	after := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 50, metadata)
	if before != after {
		t.Fatalf("reordering changed the sbt dependency id: line 5 = %q, line 50 = %q", before, after)
	}
}

// TestCanonicalEntityIDWithMetadataSbtVersionDistinctness is the #8962
// acceptance test: two declarations of one org:name in a section keep
// distinct ids across a line move.
//
// sbt appends to the libraryDependencies sequence, so the same coordinate
// can be declared twice in one section at two different revisions (a version
// bump that left both lines, or two subprojects' settings sharing one
// section literal). Both declarations are minted at the SAME line number
// here so that only the discriminator — never the source line — can keep
// them apart; the line-move half then proves the discriminator itself is
// line-independent.
func TestCanonicalEntityIDWithMetadataSbtVersionDistinctness(t *testing.T) {
	t.Parallel()

	const (
		repoID  = "repository:r_12345678"
		path    = "build.sbt"
		name    = "org.typelevel:cats-core"
		section = "libraryDependencies"
		line    = 7
	)

	first := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line,
		sbtDependencyMetadata(section, "2.10.0", "binary"))
	second := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line,
		sbtDependencyMetadata(section, "2.9.0", "binary"))

	if first == second {
		t.Fatalf("two distinct sbt revisions of the same coordinate collapsed into one id: %q", first)
	}

	if moved := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 42,
		sbtDependencyMetadata(section, "2.10.0", "binary")); moved != first {
		t.Fatalf("first declaration churned across a line move: line %d = %q, line 42 = %q", line, first, moved)
	}
	if moved := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 43,
		sbtDependencyMetadata(section, "2.9.0", "binary")); moved != second {
		t.Fatalf("second declaration churned across a line move: line %d = %q, line 43 = %q", line, second, moved)
	}
}

// TestCanonicalEntityIDWithMetadataSbtCrossVersionDistinctness proves the
// sbt-specific collision the row contract creates: name stays the unsuffixed
// organization:name for % and %% alike (sbt/README.md), so `"org" % "name"`
// and `"org" %% "name"` at the same revision in the same section share
// (section, name, value) exactly. The sbt_cross_version label must keep them
// distinct.
func TestCanonicalEntityIDWithMetadataSbtCrossVersionDistinctness(t *testing.T) {
	t.Parallel()

	const (
		repoID  = "repository:r_12345678"
		path    = "build.sbt"
		name    = "org.typelevel:cats-core"
		section = "libraryDependencies"
		line    = 7
	)

	plain := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line,
		sbtDependencyMetadata(section, "2.10.0", "none"))
	binary := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line,
		sbtDependencyMetadata(section, "2.10.0", "binary"))

	if plain == binary {
		t.Fatalf("sbt %% and %%%% declarations of the same coordinate collapsed into one id: %q", plain)
	}
}

// TestCanonicalEntityIDWithMetadataSbtConfigSectionDistinctness proves the
// #8962 issue example stays distinct: a bare declaration and a `% Test`
// declaration of one org:name land in different sections
// ("libraryDependencies" vs "libraryDependencies:Test") because the
// configuration rides in the section (sbt/rows.go), so section keying alone
// separates them — no discriminator needed for the config axis.
func TestCanonicalEntityIDWithMetadataSbtConfigSectionDistinctness(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "build.sbt"
		name   = "com.typesafe:config"
		line   = 7
	)

	compile := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line,
		sbtDependencyMetadata("libraryDependencies", "1.4.2", "none"))
	test := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line,
		sbtDependencyMetadata("libraryDependencies:Test", "1.4.2", "none"))

	if compile == test {
		t.Fatalf("compile and Test configurations collapsed into one id: %q", compile)
	}
}

// TestCanonicalEntityIDWithMetadataSbtSharedIdAcrossScalaVersions pins the
// accepted residual the sbt discriminator case discloses: two projects
// declaring the same dep at the same revision via %% agree on (section,
// name, value, sbt_cross_version), so they share one id. The parser
// computes one binary version per file (empty unless exactly one
// scalaVersion), so with one scalaVersion both rows share the same
// artifact and with two the field is empty on both: last-wins drops a
// duplicate row, never a distinct variant. The shared id is the contract,
// not a collision to fix here.
func TestCanonicalEntityIDWithMetadataSbtSharedIdAcrossScalaVersions(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "build.sbt"
		name   = "org.typelevel:cats-core"
	)
	// sbt_binary_artifact is deliberately absent: the engine emits one
	// binary version per file, so same-file rows never differ in it, and
	// the discriminator ignores that key by construction.
	scala213 := sbtDependencyMetadata("libraryDependencies", "2.10.0", "binary")
	scala3 := sbtDependencyMetadata("libraryDependencies", "2.10.0", "binary")

	first := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 11, scala213)
	second := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 40, scala3)
	if first != second {
		t.Fatalf("expected the disclosed shared id across scala versions, got %q vs %q", first, second)
	}
}
