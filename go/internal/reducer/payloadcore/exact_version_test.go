// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadcore_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// A Gradle selector names no single version, so the reducer backstop must
// refuse it as an observed version even if a parser marks the row resolved
// (issue #8988): the prefix form, latest.<status>, a range, the '!!' strict
// short-hand, the '@ext' artifact-only suffix, and a ':' leaked from string
// notation into the version text.
func TestExactManifestDependencyVersionRejectsGradleSelectors(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"+", "1.+", "1.0.+", "1.0+", "1:2",
		"latest.release", "latest.integration", "Latest.Milestone", "LATEST.RELEASE",
		"[1.0,2.0)", "(,2.0]", "]1.0,2.0[", "1.0,",
		"1.0!!", "[1.0,2.0)!!1.5",
		"1.0@jar", "1.0@aar",
	} {
		if got, ok := payloadcore.ExactManifestDependencyVersion(raw); ok {
			t.Errorf("ExactManifestDependencyVersion(%q) = %q, true; want rejected", raw, got)
		}
	}
}

// The Gradle rejects must not cost another ecosystem an exact version: semver
// build metadata carries an interior '+', and ordinary exact versions keep
// today's behavior.
func TestExactManifestDependencyVersionKeepsExactVersions(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"1.2.3", "1.2.3-SNAPSHOT", "1.2.3-rc1", "1.0.0.Final", "31.1-jre",
		"1.0.0+build.1", "0.3.1+zstd.1.5.5", "1.0-latest", "latest-release", "v1.2.3",
	} {
		if got, ok := payloadcore.ExactManifestDependencyVersion(raw); !ok || got != raw {
			t.Errorf("ExactManifestDependencyVersion(%q) = %q, %v; want %q, true", raw, got, ok, raw)
		}
	}
}
