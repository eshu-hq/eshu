// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/gradle"
	"github.com/eshu-hq/eshu/go/internal/parser/maven"
	"github.com/eshu-hq/eshu/go/internal/parser/nodelockfile"
	"github.com/eshu-hq/eshu/go/internal/parser/ruby"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestDependencyCoverageCoveredJSONFilesEmitDependencyRows binds every Covered
// matrix entry that this JSON package owns to a runtime fixture so the
// documented capability is provable instead of asserted. Non-JSON parsers are
// exercised by the parent engine coverage test.
func TestDependencyCoverageCoveredJSONFilesEmitDependencyRows(t *testing.T) {
	t.Parallel()

	fixtures := dependencyCoverageCoveredFixtures()
	nonJSONCovered := map[string]bool{
		"requirements.txt": true,
		"pyproject.toml":   true,
		"pipfile":          true,
		"poetry.lock":      true,
	}
	for _, entry := range DependencyCoverage() {
		if entry.Status != DependencyCoverageCovered {
			continue
		}
		if strings.Contains(entry.FilePattern, "*") || entry.Ecosystem == "cargo" {
			continue
		}
		if nonJSONCovered[entry.FilePattern] {
			continue
		}
		fixture, ok := fixtures[entry.FilePattern]
		if !ok {
			continue
		}
		assertCoveredFixtureRows(t, entry, fixture)
	}
}

type coveredFixture struct {
	parser                func(*testing.T, string, string) (map[string]any, error)
	body                  string
	expectedDependencies  map[string]string
	expectedPackageMgr    string
	expectedSection       string
	expectScopeSplit      bool
	expectedDevDependency string
	transitiveBody        string
}

func assertCoveredFixtureRows(t *testing.T, entry DependencyCoverageEntry, fixture coveredFixture) {
	t.Helper()
	payload, err := fixture.parser(t, entry.FilePattern, fixture.body)
	if err != nil {
		t.Fatalf("%s: Parse() error = %v", entry.FilePattern, err)
	}
	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("%s: variables payload missing (got %T)", entry.FilePattern, payload["variables"])
	}
	rowsByName := dependencyRowsByName(rows)
	for name, wantValue := range fixture.expectedDependencies {
		row, ok := rowsByName[name]
		if !ok {
			t.Fatalf("%s: dependency %q missing from variables %#v", entry.FilePattern, name, rows)
		}
		if row["config_kind"] != "dependency" {
			t.Fatalf("%s: %q config_kind = %#v, want dependency", entry.FilePattern, name, row["config_kind"])
		}
		if row["package_manager"] != fixture.expectedPackageMgr {
			t.Fatalf("%s: %q package_manager = %#v, want %q", entry.FilePattern, name, row["package_manager"], fixture.expectedPackageMgr)
		}
		if row["value"] != wantValue {
			t.Fatalf("%s: %q value = %#v, want %q", entry.FilePattern, name, row["value"], wantValue)
		}
	}
	assertCoveredFixtureScopeSplit(t, entry, rowsByName, fixture)
	assertCoveredFixtureDependencyChain(t, entry, rows, fixture)
}

func assertCoveredFixtureScopeSplit(
	t *testing.T,
	entry DependencyCoverageEntry,
	rowsByName map[string]map[string]any,
	fixture coveredFixture,
) {
	t.Helper()
	if !fixture.expectScopeSplit {
		return
	}
	row, ok := rowsByName[fixture.expectedDevDependency]
	if !ok {
		t.Fatalf("%s: dev dependency %q missing", entry.FilePattern, fixture.expectedDevDependency)
	}
	section, _ := row["section"].(string)
	if section == "" || section == fixture.expectedSection {
		t.Fatalf("%s: dev dependency %q section = %q, want a distinct dev scope", entry.FilePattern, fixture.expectedDevDependency, section)
	}
	if !entry.CapturesDevRuntimeSplit {
		t.Fatalf("%s: parser distinguishes dev scope but matrix says it does not", entry.FilePattern)
	}
}

func assertCoveredFixtureDependencyChain(
	t *testing.T,
	entry DependencyCoverageEntry,
	rows []map[string]any,
	fixture coveredFixture,
) {
	t.Helper()
	if !entry.CapturesDependencyChain {
		return
	}
	if fixtureHasTransitiveDependencyPath(rows) {
		return
	}
	if len(fixture.expectedDependencies) > 0 && fixture.transitiveBody != "" {
		rerun, err := fixture.parser(t, entry.FilePattern, fixture.transitiveBody)
		if err != nil {
			t.Fatalf("%s: transitive Parse error = %v", entry.FilePattern, err)
		}
		rerunRows, _ := rerun["variables"].([]map[string]any)
		if fixtureHasTransitiveDependencyPath(rerunRows) {
			return
		}
	}
	t.Fatalf("%s: matrix claims dependency chain capture but parser produced no dependency_path", entry.FilePattern)
}

func fixtureHasTransitiveDependencyPath(rows []map[string]any) bool {
	for _, row := range rows {
		if path, ok := row["dependency_path"].([]string); ok && len(path) > 1 {
			return true
		}
	}
	return false
}

func parseJSONFixture(t *testing.T, filename, body string) (map[string]any, error) {
	t.Helper()
	path := writeJSONTestFile(t, filename, body)
	return Parse(path, false, shared.Options{}, Config{})
}

func parseMavenFixture(t *testing.T, filename, body string) (map[string]any, error) {
	t.Helper()
	path := writeJSONTestFile(t, filename, body)
	return maven.Parse(path, false, shared.Options{})
}

func parseGradleFixture(t *testing.T, filename, body string) (map[string]any, error) {
	t.Helper()
	path := writeJSONTestFile(t, filename, body)
	return gradle.Parse(path, false, shared.Options{})
}

func parseRubyFixture(t *testing.T, filename, body string) (map[string]any, error) {
	t.Helper()
	path := writeJSONTestFile(t, filename, body)
	return ruby.Parse(path, false, shared.Options{})
}

func parseNodeLockfileFixture(t *testing.T, filename, body string) (map[string]any, error) {
	t.Helper()
	path := writeJSONTestFile(t, filename, body)
	return nodelockfile.Parse(path, false, shared.Options{})
}

func dependencyCoverageCoveredFixtures() map[string]coveredFixture {
	return map[string]coveredFixture{
		"package.json":       packageJSONCoveredFixture(),
		"package-lock.json":  packageLockCoveredFixture(),
		"composer.json":      composerManifestCoveredFixture(),
		"composer.lock":      composerLockCoveredFixture(),
		"gemfile":            gemfileCoveredFixture(),
		"gemfile.lock":       gemfileLockCoveredFixture(),
		"packages.lock.json": nugetLockCoveredFixture(),
		"Package.resolved":   swiftPackageResolvedCoveredFixture(),
		"pom.xml":            mavenCoveredFixture(),
		"build.gradle":       gradleCoveredFixture(),
		"build.gradle.kts":   gradleKTSCoveredFixture(),
		"pipfile.lock":       pipfileLockCoveredFixture(),
		"yarn.lock":          yarnCoveredFixture(),
		"pnpm-lock.yaml":     pnpmCoveredFixture(),
	}
}
