// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nodelockfile

import (
	"testing"
)

// TestParsePnpmLockfileEmitsExactVersions covers pnpm-lock.yaml v6+ format
// with the importers / packages sections. Scoped packages must keep their
// "@scope/name" identity intact, and dev dependencies must carry a distinct
// scope so the consumption reducer can bound impact to runtime when needed.
func TestParsePnpmLockfileEmitsExactVersions(t *testing.T) {
	t.Parallel()

	path := writeTestFile(t, "pnpm-lock.yaml", `lockfileVersion: '6.0'

settings:
  autoInstallPeers: true

importers:
  .:
    dependencies:
      lodash:
        specifier: ^4.17.21
        version: 4.17.21
      '@scope/api':
        specifier: ^1.0.0
        version: 1.2.3
    devDependencies:
      vitest:
        specifier: ^2.0.0
        version: 2.0.0(@types/node@20.5.0)

packages:

  /lodash@4.17.21:
    resolution: {integrity: sha512-AbCdEf==}

  /@scope/api@1.2.3:
    resolution: {integrity: sha512-XyZ==}

  /vitest@2.0.0(@types/node@20.5.0):
    resolution: {integrity: sha512-Vit==}
    dependencies:
      vite: 5.0.0

  /vite@5.0.0:
    resolution: {integrity: sha512-V==}
`)

	rows := parseAndExtractRows(t, path)
	got := rowsByName(rows)

	wantDirect := map[string]string{
		"lodash":     "4.17.21",
		"@scope/api": "1.2.3",
		"vitest":     "2.0.0",
	}
	for name, version := range wantDirect {
		row, ok := got[name]
		if !ok {
			t.Fatalf("expected pnpm dependency row for %q in %#v", name, rows)
		}
		if row["value"] != version {
			t.Fatalf("%s value = %#v, want %q", name, row["value"], version)
		}
		if row["package_manager"] != "npm" {
			t.Fatalf("%s package_manager = %#v, want canonical npm ecosystem", name, row["package_manager"])
		}
		if row["package_manager_flavor"] != "pnpm" {
			t.Fatalf("%s package_manager_flavor = %#v, want pnpm", name, row["package_manager_flavor"])
		}
		if row["lockfile_format"] != "pnpm" {
			t.Fatalf("%s lockfile_format = %#v, want pnpm", name, row["lockfile_format"])
		}
	}

	// vitest is dev; lodash is runtime. The two must carry distinct sections
	// so reducers that bound impact to runtime can split correctly.
	if got["vitest"]["section"] == got["lodash"]["section"] {
		t.Fatalf("expected pnpm dev vs runtime to have distinct sections, got %q and %q",
			got["vitest"]["section"], got["lodash"]["section"])
	}

	// Transitive package (vite) is recorded even though it has no importer entry,
	// so vulnerability impact can walk transitive evidence.
	if _, ok := got["vite"]; !ok {
		t.Fatalf("expected transitive pnpm dependency row for vite in %#v", rows)
	}
}

// TestParsePnpmLockfilePreservesDependencyChain pins transitive parent
// evidence for pnpm: vitest depends on vite, so the vite row must carry a
// dependency_path showing the importer entry path.
func TestParsePnpmLockfilePreservesDependencyChain(t *testing.T) {
	t.Parallel()

	path := writeTestFile(t, "pnpm-lock.yaml", `lockfileVersion: '6.0'

importers:
  .:
    dependencies:
      vite:
        specifier: ^5.0.0
        version: 5.0.0

packages:

  /vite@5.0.0:
    resolution: {integrity: sha512-V==}
    dependencies:
      rollup: 4.0.0

  /rollup@4.0.0:
    resolution: {integrity: sha512-R==}
    dependencies:
      fsevents: 2.3.3

  /fsevents@2.3.3:
    resolution: {integrity: sha512-F==}
`)

	got := rowsByName(parseAndExtractRows(t, path))
	assertChain(t, got["vite"], []string{"vite"}, 1, true)
	assertChain(t, got["rollup"], []string{"vite", "rollup"}, 2, false)
	assertChain(t, got["fsevents"], []string{"vite", "rollup", "fsevents"}, 3, false)
}

// TestParsePnpmLockfileWorkspaceEntriesAreNotRemotePackages enforces the
// "Out of scope" rule from issue #644: a workspace/local path dependency must
// not be treated as a remote registry version. The pnpm `link:` and
// `workspace:` protocols mark local code; the lockfile does not prove a
// remote package identity.
func TestParsePnpmLockfileWorkspaceEntriesAreNotRemotePackages(t *testing.T) {
	t.Parallel()

	path := writeTestFile(t, "pnpm-lock.yaml", `lockfileVersion: '6.0'

importers:
  .:
    dependencies:
      shared-utils:
        specifier: workspace:*
        version: link:../shared-utils
      app-config:
        specifier: file:./local/config
        version: file:./local/config
      lodash:
        specifier: ^4.17.21
        version: 4.17.21

packages:

  /lodash@4.17.21:
    resolution: {integrity: sha512-AbCdEf==}
`)

	got := rowsByName(parseAndExtractRows(t, path))

	if _, ok := got["shared-utils"]; ok {
		t.Fatalf("workspace link dependency was admitted as remote: %#v", got["shared-utils"])
	}
	if _, ok := got["app-config"]; ok {
		t.Fatalf("file: local dependency was admitted as remote: %#v", got["app-config"])
	}
	if _, ok := got["lodash"]; !ok {
		t.Fatalf("expected remote lodash dependency to still be admitted alongside workspace siblings: %#v", got)
	}
}

func TestParsePnpmLockfilePreservesOptionalAndPeerImporterScopes(t *testing.T) {
	t.Parallel()

	path := writeTestFile(t, "pnpm-lock.yaml", `lockfileVersion: '6.0'

importers:
  .:
    optionalDependencies:
      fsevents:
        specifier: ^2.3.3
        version: 2.3.3
    peerDependencies:
      react:
        specifier: '>=18'
        version: 18.3.1

packages:

  /fsevents@2.3.3:
    resolution: {integrity: sha512-F==}

  /react@18.3.1:
    resolution: {integrity: sha512-R==}
`)

	got := rowsByName(parseAndExtractRows(t, path))
	if got["fsevents"]["section"] != "optional" {
		t.Fatalf("fsevents section = %#v, want optional", got["fsevents"]["section"])
	}
	if got["react"]["section"] != "peer" {
		t.Fatalf("react section = %#v, want peer", got["react"]["section"])
	}
	assertChain(t, got["fsevents"], []string{"fsevents"}, 1, true)
	assertChain(t, got["react"], []string{"react"}, 1, true)
}
