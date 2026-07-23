// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content

import "testing"

// mavenDependencyMetadata builds the entity_metadata a pom.xml dependency row
// (maven/parser.go's buildDependencyRow) contributes.
func mavenDependencyMetadata(section, classifier, depType string) map[string]any {
	metadata := map[string]any{
		"config_kind":     "dependency",
		"package_manager": "maven",
		"section":         section,
	}
	if classifier != "" {
		metadata["dependency_classifier"] = classifier
	}
	if depType != "" {
		metadata["dependency_type"] = depType
	}
	return metadata
}

// TestCanonicalEntityIDWithMetadataMavenAdmitsInScopeRow proves an ordinary
// Maven POM dependency row (#5507) routes to the section-keyed scheme.
func TestCanonicalEntityIDWithMetadataMavenAdmitsInScopeRow(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "pom.xml"
		name   = "com.fasterxml.jackson.core:jackson-databind"
		line   = 20
	)
	metadata := mavenDependencyMetadata("dependencies", "", "")

	got := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line, metadata)
	if legacy := CanonicalEntityID(repoID, path, "Variable", name, line); got == legacy {
		t.Fatalf("CanonicalEntityIDWithMetadata() = %q unexpectedly matched legacy CanonicalEntityID() for an in-scope maven row", got)
	}
}

// TestCanonicalEntityIDWithMetadataMavenReorderNoChurn proves a Maven
// dependency's identity is stable when its <dependency> element moves.
func TestCanonicalEntityIDWithMetadataMavenReorderNoChurn(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "pom.xml"
		name   = "com.fasterxml.jackson.core:jackson-databind"
	)
	metadata := mavenDependencyMetadata("dependencies", "", "")

	before := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 3, metadata)
	after := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 40, metadata)
	if before != after {
		t.Fatalf("reordering changed the maven dependency id: line 3 = %q, line 40 = %q", before, after)
	}
}

// TestCanonicalEntityIDWithMetadataMavenClassifierDistinctness proves the
// case #5507 flagged for maven: the same groupId:artifactId is routinely
// declared more than once in the same <dependencies> section with a
// different <classifier> for co-installed platform-native builds (e.g.
// netty-tcnative's linux-x86_64 and osx-x86_64 classifiers, both required at
// once). (section, name) alone would collapse them; the classifier
// discriminator must keep them distinct.
func TestCanonicalEntityIDWithMetadataMavenClassifierDistinctness(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "pom.xml"
		name   = "io.netty:netty-tcnative-boringssl-static"
	)

	linux := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 10,
		mavenDependencyMetadata("dependencies", "linux-x86_64", ""))
	osx := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 11,
		mavenDependencyMetadata("dependencies", "osx-x86_64", ""))

	if linux == osx {
		t.Fatalf("two distinct maven classifiers of the same coordinate collapsed into one id: %q", linux)
	}
}

// TestCanonicalEntityIDWithMetadataMavenTypeDistinctness proves a jar
// dependency stays distinct from its test-jar counterpart declared under the
// same groupId:artifactId in the same section.
func TestCanonicalEntityIDWithMetadataMavenTypeDistinctness(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "pom.xml"
		name   = "com.example:widgets"
	)

	jar := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 10,
		mavenDependencyMetadata("dependencies", "", "jar"))
	testJar := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 11,
		mavenDependencyMetadata("dependencies", "", "test-jar"))

	if jar == testJar {
		t.Fatalf("jar and test-jar types of the same coordinate collapsed into one id: %q", jar)
	}
}

// TestCanonicalEntityIDWithMetadataMavenImplicitJarTypeNoChurn proves that
// adding an explicit <type>jar</type> to a dependency that previously omitted
// <type> (implicit jar, Maven's own default) does not change its identity —
// the discriminator normalizes an absent type to "jar" specifically to avoid
// this churn.
func TestCanonicalEntityIDWithMetadataMavenImplicitJarTypeNoChurn(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "pom.xml"
		name   = "com.example:widgets"
		line   = 10
	)

	implicit := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line,
		mavenDependencyMetadata("dependencies", "", ""))
	explicit := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line,
		mavenDependencyMetadata("dependencies", "", "jar"))

	if implicit != explicit {
		t.Fatalf("explicit <type>jar</type> churned identity vs. implicit default: implicit=%q explicit=%q", implicit, explicit)
	}
}

// TestCanonicalEntityIDWithMetadataMavenScopeSectionDistinctness proves a
// dependency declared under the default <dependencies> section stays
// distinct from the same coordinate declared under <dependencyManagement>.
func TestCanonicalEntityIDWithMetadataMavenScopeSectionDistinctness(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "pom.xml"
		name   = "com.example:widgets"
		line   = 10
	)

	direct := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line,
		mavenDependencyMetadata("dependencies", "", ""))
	managed := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line,
		mavenDependencyMetadata("dependencyManagement", "", ""))

	if direct == managed {
		t.Fatalf("dependencies and dependencyManagement sections collapsed into one id: %q", direct)
	}
}
