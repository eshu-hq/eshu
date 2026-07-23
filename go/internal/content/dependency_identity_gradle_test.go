// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content

import "testing"

// gradleDependencyMetadata builds the entity_metadata a build.gradle(.kts)
// dependency row (gradle/statement.go's buildRow) contributes.
func gradleDependencyMetadata(section, value string) map[string]any {
	return map[string]any{
		"config_kind":     "dependency",
		"package_manager": "gradle",
		"section":         section,
		"value":           value,
	}
}

// TestCanonicalEntityIDWithMetadataGradleAdmitsInScopeRow proves an ordinary
// Gradle dependency row (#5507) routes to the section-keyed scheme.
func TestCanonicalEntityIDWithMetadataGradleAdmitsInScopeRow(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "build.gradle"
		name   = "com.google.guava:guava"
		line   = 12
	)
	metadata := gradleDependencyMetadata("implementation", "31.1-jre")

	got := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line, metadata)
	if legacy := CanonicalEntityID(repoID, path, "Variable", name, line); got == legacy {
		t.Fatalf("CanonicalEntityIDWithMetadata() = %q unexpectedly matched legacy CanonicalEntityID() for an in-scope gradle row", got)
	}
}

// TestCanonicalEntityIDWithMetadataGradleReorderNoChurn proves a Gradle
// dependency's identity is stable when its declaration moves within the file.
func TestCanonicalEntityIDWithMetadataGradleReorderNoChurn(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "build.gradle"
		name   = "com.google.guava:guava"
	)
	metadata := gradleDependencyMetadata("implementation", "31.1-jre")

	before := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 5, metadata)
	after := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 50, metadata)
	if before != after {
		t.Fatalf("reordering changed the gradle dependency id: line 5 = %q, line 50 = %q", before, after)
	}
}

// TestCanonicalEntityIDWithMetadataGradleVersionDistinctness proves the case
// #5507 flagged for gradle: the same group:artifact coordinate can be
// declared twice under the identical configuration, each at a different
// version (e.g. a pinned exact version alongside a looser range added later
// without deleting the first line). (section, name) alone would collapse
// them; the resolved "value" (version) discriminator must keep them distinct.
func TestCanonicalEntityIDWithMetadataGradleVersionDistinctness(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "build.gradle"
		name   = "com.google.guava:guava"
	)

	pinned := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 5,
		gradleDependencyMetadata("implementation", "31.1-jre"))
	ranged := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 6,
		gradleDependencyMetadata("implementation", "31.+"))

	if pinned == ranged {
		t.Fatalf("two distinct gradle versions of the same coordinate collapsed into one id: %q", pinned)
	}
}

// TestCanonicalEntityIDWithMetadataGradleCrossConfigurationDistinctness
// proves a coordinate declared under `implementation` stays distinct from the
// same coordinate declared under `testImplementation`.
func TestCanonicalEntityIDWithMetadataGradleCrossConfigurationDistinctness(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "build.gradle"
		name   = "com.google.guava:guava"
		line   = 5
	)

	implementation := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line,
		gradleDependencyMetadata("implementation", "31.1-jre"))
	testImplementation := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line,
		gradleDependencyMetadata("testImplementation", "31.1-jre"))

	if implementation == testImplementation {
		t.Fatalf("implementation and testImplementation configurations collapsed into one id: %q", implementation)
	}
}
