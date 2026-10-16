// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript_test

import (
	"path/filepath"
	"testing"
)

// TestDefaultEngineParsePathStampsNoProducerKeyUnderFixtureManifest pins the
// #8244 parser half through the real engine: a class beside a package.json
// under a fixture-style directory gets no package_id even when the manifest
// repeats the real package's name, and the real root class still does.
func TestDefaultEngineParsePathStampsNoProducerKeyUnderFixtureManifest(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	const source = "export class Greeter {}\n"
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{"name":"acme-lib","main":"src/index.js"}`)
	writeTestFile(t, filepath.Join(repoRoot, "src", "index.js"), source)
	writeTestFile(t, filepath.Join(repoRoot, "tests", "fixtures", "app", "package.json"), `{"name":"acme-lib","main":"src/index.js"}`)
	writeTestFile(t, filepath.Join(repoRoot, "tests", "fixtures", "app", "src", "index.js"), source)

	rootID, rootExport := definitionKeys(t, parsePackageKeyFixture(t, repoRoot, filepath.Join(repoRoot, "src", "index.js")), "classes", "Greeter")
	if rootID != "acme-lib" || rootExport != "Greeter" {
		t.Fatalf("root class keys = (%q, %q), want (acme-lib, Greeter)", rootID, rootExport)
	}
	fixturePath := filepath.Join(repoRoot, "tests", "fixtures", "app", "src", "index.js")
	fixtureID, fixtureExport := definitionKeys(t, parsePackageKeyFixture(t, repoRoot, fixturePath), "classes", "Greeter")
	if fixtureID != "" || fixtureExport != "" {
		t.Fatalf("fixture class keys = (%q, %q), want none", fixtureID, fixtureExport)
	}
}
