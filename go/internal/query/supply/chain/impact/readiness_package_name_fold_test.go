// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"regexp"
	"strings"
	"testing"
)

// readinessPackageNameFoldSQL is the package-name fold the derived OSV source
// state lookup must use (#9584). It equals packageidentity.LowerASCII, the
// fold that wrote vulnerability_source_state_packages.
const readinessPackageNameFoldSQL = `LOWER(TRIM(consumption.payload->>'package_name') COLLATE "C") COLLATE "default"`

// readinessLocaleLowerPattern matches a LOWER(TRIM(...)) fold with no
// COLLATE inside it, that is a fold that follows the database collation.
var readinessLocaleLowerPattern = regexp.MustCompile(`LOWER\(TRIM\([^()]*\)\)`)

// TestReadinessDerivedSourceStateFoldsASCIIOnly pins the #9584 SQL contract:
// the derived source-state CTE folds package names and ecosystem spellings
// with LOWER under COLLATE "C", never a locale LOWER that maps U+0130 to i and
// U+212A to k.
func TestReadinessDerivedSourceStateFoldsASCIIOnly(t *testing.T) {
	t.Parallel()

	if !strings.Contains(readinessDerivedSourceStateCTE, readinessPackageNameFoldSQL) {
		t.Fatalf("readinessDerivedSourceStateCTE does not fold package_name with %s", readinessPackageNameFoldSQL)
	}
	// Seeded-violation pair: the guard must fire on each locale fold the CTE
	// used before #9584 and stay quiet on the shipped COLLATE "C" forms, so a
	// pattern that matches nothing cannot pass for a guard.
	for _, violation := range []string{
		"alias.spelling = LOWER(TRIM(configured.ecosystem))",
		"LOWER(TRIM(consumption.payload->>'package_name'))",
		"alias.spelling = LOWER(TRIM(consumption.payload->>'ecosystem'))",
	} {
		if !readinessLocaleLowerPattern.MatchString(violation) {
			t.Fatalf("locale-fold guard does not fire on %q", violation)
		}
	}
	for _, shipped := range []string{
		readinessPackageNameFoldSQL,
		`LOWER(TRIM(configured.ecosystem) COLLATE "C") COLLATE "default"`,
	} {
		if readinessLocaleLowerPattern.MatchString(shipped) {
			t.Fatalf("locale-fold guard fires on the shipped fold %q", shipped)
		}
	}
	if got := readinessLocaleLowerPattern.FindAllString(readinessDerivedSourceStateCTE, -1); len(got) > 0 {
		t.Fatalf("readinessDerivedSourceStateCTE keeps locale LOWER folds %q; use LOWER(TRIM(x) COLLATE \"C\") COLLATE \"default\"", got)
	}
}
