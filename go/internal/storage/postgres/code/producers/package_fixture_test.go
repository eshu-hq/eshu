// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package producerstore

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/php/composer"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestPackageManifestsQueryShape pins the package.json read's load-bearing
// parts: the MATERIALIZED CTE, the path predicates, the node_modules
// exclusion, and the #8244 fixture-directory exclusion built from the one
// pattern constant the parser's NearestPackageName also applies.
func TestPackageManifestsQueryShape(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"WITH manifest AS MATERIALIZED (",
		"relative_path = 'package.json' OR relative_path LIKE '%/package.json'",
		"relative_path !~* '(^|/)node_modules[^/]*/'",
		"relative_path !~ '" + shared.FixtureDirPattern + "'",
		"WHEN tagged.tag_activated_at IS NOT NULL THEN 'clean'",
	} {
		if !strings.Contains(PackageManifestsQuery, want) {
			t.Fatalf("PackageManifestsQuery missing %q", want)
		}
	}
	if strings.Contains(PackageManifestsQuery, "!~*") && strings.Contains(PackageManifestsQuery, "(?i)") {
		t.Fatal("PackageManifestsQuery must not use a case-fold flag for the fixture rule")
	}
	if strings.Contains(PackageManifestsQuery, "composer.json") {
		t.Fatal("PackageManifestsQuery must not read composer.json")
	}
}

// TestFixtureDirPatternIsOneConstantAcrossEcosystems is the lockstep guard:
// the composer anchor and the package.json anchor must apply one pattern, not
// two hand-copied strings.
func TestFixtureDirPatternIsOneConstantAcrossEcosystems(t *testing.T) {
	t.Parallel()

	if composer.FixtureDirPattern != shared.FixtureDirPattern {
		t.Fatalf("composer.FixtureDirPattern = %q, shared.FixtureDirPattern = %q: must be one constant", composer.FixtureDirPattern, shared.FixtureDirPattern)
	}
	if !strings.Contains(ComposerManifestsQuery, "relative_path !~ '"+shared.FixtureDirPattern+"'") {
		t.Fatal("ComposerManifestsQuery must embed shared.FixtureDirPattern")
	}
}
