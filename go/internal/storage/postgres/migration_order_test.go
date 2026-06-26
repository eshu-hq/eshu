// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// firstNonDecreasingViolation returns a diagnostic message if any Definition
// in defs has a Path filename whose numeric version prefix decreases relative
// to the previous definition. The version is extracted from the filename as
// the leading digits (and optional letter suffix) before the underscore.
// Returns ("", false) if the sequence is non-decreasing (the normal case).
func firstNonDecreasingViolation(defs []Definition) (msg string, ok bool) {
	var prevVersion int
	for i, def := range defs {
		base := filepath.Base(def.Path)
		versionStr, _, found := strings.Cut(base, "_")
		if !found {
			return fmt.Sprintf(
				"definition[%d] Name=%q Path=%q: filename %q has no underscore separator; expected pattern NNN_name.sql",
				i, def.Name, def.Path, base,
			), true
		}

		// Strip optional letter suffix (e.g. "003a" -> "003") for number comparison.
		numericPart := strings.TrimRight(versionStr, "abcdefghijklmnopqrstuvwxyz")
		version, err := strconv.Atoi(numericPart)
		if err != nil {
			return fmt.Sprintf(
				"definition[%d] Name=%q Path=%q: version prefix %q is not a valid integer: %v",
				i, def.Name, def.Path, versionStr, err,
			), true
		}

		if i > 0 && version < prevVersion {
			return fmt.Sprintf(
				"definition[%d] Name=%q Path=%q version=%d: version decreased from previous definition[%d] Name=%q Path=%q version=%d",
				i, def.Name, def.Path, version,
				i-1, defs[i-1].Name, defs[i-1].Path, prevVersion,
			), true
		}

		prevVersion = version
	}
	return "", false
}

// TestMigrationPathsAreNonDecreasing asserts that every Definition in
// BootstrapDefinitions() has a Path filename whose numeric version prefix
// follows a non-decreasing sequence (e.g. 001, 002, 003a, 003b, ...).
// Because BootstrapDefinitions() already sorts by Path, this test catches
// non-zero-padded names (e.g. "9_foo.sql" lexically sorts after "10_foo.sql"
// but has version 9 < 10), missing underscores, and non-numeric prefixes.
func TestMigrationPathsAreNonDecreasing(t *testing.T) {
	t.Parallel()

	defs := BootstrapDefinitions()
	if len(defs) == 0 {
		t.Fatal("BootstrapDefinitions() returned zero definitions")
	}

	if msg, violated := firstNonDecreasingViolation(defs); violated {
		t.Fatal(msg)
	}
}

// TestMigrationOrderingPlantBadOrderFails confirms that firstNonDecreasingViolation
// detects a deliberately misordered pair — proving the shared helper (and by
// extension the production ordering check) actually catches ordering violations.
func TestMigrationOrderingPlantBadOrderFails(t *testing.T) {
	t.Parallel()

	badDefs := []Definition{
		{Name: "second", Path: "schema/data-plane/postgres/010_second.sql", SQL: "SELECT 1"},
		{Name: "first", Path: "schema/data-plane/postgres/005_first.sql", SQL: "SELECT 1"},
	}

	msg, violated := firstNonDecreasingViolation(badDefs)
	if !violated {
		t.Fatal("planted bad-ordering pair (010 before 005) was NOT detected by firstNonDecreasingViolation; assertion is broken")
	}

	// The message must identify the correct offending index and versions.
	if !strings.Contains(msg, "005") || !strings.Contains(msg, "010") {
		t.Fatalf("violation message does not mention the offending versions: %s", msg)
	}
}
