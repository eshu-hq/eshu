// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nodelockfile

import (
	"testing"
)

// TestParseYarnClassicLockfileEmitsRowPerVersionForDuplicateNames pins the
// Copilot review finding from PR #664: yarn lockfiles routinely contain
// multiple blocks for the same package name when different ranges resolve
// to different versions. Keying internal maps only by name silently
// overwrites versions and edges. Each (name, version) instance must show
// up as its own dependency row so vulnerability impact can see all
// installed copies.
func TestParseYarnClassicLockfileEmitsRowPerVersionForDuplicateNames(t *testing.T) {
	t.Parallel()

	path := writeTestFile(t, "yarn.lock", `# yarn lockfile v1

lodash@^4.17.21:
  version "4.17.21"
  resolved "https://registry.yarnpkg.com/lodash/-/lodash-4.17.21.tgz"

lodash@^3.0.0:
  version "3.10.1"
  resolved "https://registry.yarnpkg.com/lodash/-/lodash-3.10.1.tgz"

modern-lib@^1.0.0:
  version "1.0.0"
  dependencies:
    lodash "^4.17.21"

legacy-lib@^1.0.0:
  version "1.0.0"
  dependencies:
    lodash "^3.0.0"
`)

	rows := parseAndExtractRows(t, path)
	versions := versionsForName(rows, "lodash")
	if len(versions) != 2 {
		t.Fatalf("yarn.lock produced %d lodash rows, want 2 distinct versions; got versions=%v rows=%#v",
			len(versions), versions, rows)
	}
	if !versions["4.17.21"] {
		t.Fatalf("missing lodash@4.17.21 row in versions=%v rows=%#v", versions, rows)
	}
	if !versions["3.10.1"] {
		t.Fatalf("missing lodash@3.10.1 row in versions=%v rows=%#v", versions, rows)
	}
}

// TestParseYarnBerryLockfileEmitsRowPerVersionForDuplicateNames covers the
// Berry analogue: two resolutions for the same package name must produce
// two rows, not silently overwrite each other.
func TestParseYarnBerryLockfileEmitsRowPerVersionForDuplicateNames(t *testing.T) {
	t.Parallel()

	path := writeTestFile(t, "yarn.lock", `__metadata:
  version: 8

"lodash@npm:^4.17.21":
  version: 4.17.21
  resolution: "lodash@npm:4.17.21"
  languageName: node
  linkType: hard

"lodash@npm:^3.0.0":
  version: 3.10.1
  resolution: "lodash@npm:3.10.1"
  languageName: node
  linkType: hard
`)

	rows := parseAndExtractRows(t, path)
	versions := versionsForName(rows, "lodash")
	if len(versions) != 2 {
		t.Fatalf("yarn berry produced %d lodash rows, want 2; got versions=%v rows=%#v",
			len(versions), versions, rows)
	}
	if !versions["4.17.21"] || !versions["3.10.1"] {
		t.Fatalf("missing one or both lodash versions in %v", versions)
	}
}

// TestParsePnpmLockfileEmitsRowPerVersionForDuplicateNames covers the pnpm
// analogue: pnpm package keys already encode version, but our internal
// importer map must not collapse multiple versions into one row.
func TestParsePnpmLockfileEmitsRowPerVersionForDuplicateNames(t *testing.T) {
	t.Parallel()

	path := writeTestFile(t, "pnpm-lock.yaml", `lockfileVersion: '6.0'

importers:
  .:
    dependencies:
      modern-lib:
        specifier: ^1.0.0
        version: 1.0.0
      legacy-lib:
        specifier: ^1.0.0
        version: 1.0.0

packages:

  /modern-lib@1.0.0:
    resolution: {integrity: sha512-A==}
    dependencies:
      lodash: 4.17.21

  /legacy-lib@1.0.0:
    resolution: {integrity: sha512-B==}
    dependencies:
      lodash: 3.10.1

  /lodash@4.17.21:
    resolution: {integrity: sha512-C==}

  /lodash@3.10.1:
    resolution: {integrity: sha512-D==}
`)

	rows := parseAndExtractRows(t, path)
	versions := versionsForName(rows, "lodash")
	if len(versions) != 2 {
		t.Fatalf("pnpm produced %d lodash rows, want 2; got versions=%v rows=%#v",
			len(versions), versions, rows)
	}
	if !versions["4.17.21"] || !versions["3.10.1"] {
		t.Fatalf("missing one or both lodash versions in %v", versions)
	}
}

// versionsForName returns the set of resolved versions emitted for a single
// package name across the parser payload's variables bucket.
func versionsForName(rows []map[string]any, name string) map[string]bool {
	out := make(map[string]bool)
	for _, row := range rows {
		rowName, _ := row["name"].(string)
		if rowName != name {
			continue
		}
		version, _ := row["value"].(string)
		if version != "" {
			out[version] = true
		}
	}
	return out
}
