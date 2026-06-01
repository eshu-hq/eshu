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

// TestDependencyCoverageMatrixIsStableAndExhaustive guards the contract
// behind issue #571. The repository dependency coverage matrix is the only
// place that names which ecosystem manifests and lockfiles produce
// content_entity dependency facts. Every entry must declare a status, and the
// matrix must keep at least one Covered entry per emitter we already ship so
// the supply-chain readiness story does not silently regress.
func TestDependencyCoverageMatrixIsStableAndExhaustive(t *testing.T) {
	t.Parallel()

	entries := DependencyCoverage()
	if len(entries) == 0 {
		t.Fatalf("DependencyCoverage() returned no entries")
	}

	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.Ecosystem == "" {
			t.Fatalf("entry %q has empty Ecosystem", entry.FilePattern)
		}
		if entry.FilePattern == "" {
			t.Fatalf("entry in ecosystem %q has empty FilePattern", entry.Ecosystem)
		}
		key := entry.Ecosystem + "|" + entry.FilePattern
		if _, ok := seen[key]; ok {
			t.Fatalf("duplicate coverage entry %q", key)
		}
		seen[key] = struct{}{}

		switch entry.Status {
		case DependencyCoverageCovered:
			if !entry.CapturesPackageIdentity {
				t.Fatalf("%s: covered entries must capture package identity", key)
			}
			if !entry.CapturesExactVersion && !entry.CapturesVersionRange {
				t.Fatalf("%s: covered entries must capture exact version or range", key)
			}
			if entry.SourceReference == "" {
				t.Fatalf("%s: covered entries must cite a SourceReference", key)
			}
		case DependencyCoverageGap:
			if entry.CapturesPackageIdentity ||
				entry.CapturesExactVersion ||
				entry.CapturesVersionRange ||
				entry.CapturesScope ||
				entry.CapturesDevRuntimeSplit ||
				entry.CapturesDependencyChain {
				t.Fatalf("%s: gap entries must not claim captured fields (got %#v)", key, entry)
			}
			if entry.Notes == "" {
				t.Fatalf("%s: gap entries must explain the missing-evidence consequence", key)
			}
		default:
			t.Fatalf("%s: unknown status %q", key, entry.Status)
		}
	}

	requiredCovered := []string{
		"npm|package.json",
		"npm|package-lock.json",
		"composer|composer.json",
		"composer|composer.lock",
		"go|go.mod",
		"nuget|*.csproj",
		"nuget|packages.lock.json",
		"rubygems|gemfile",
		"rubygems|gemfile.lock",
		"cargo|cargo.toml",
		"cargo|cargo.lock",
		"swift|Package.resolved",
		"maven|pom.xml",
		"gradle|build.gradle",
		"gradle|build.gradle.kts",
		"pypi|requirements.txt",
		"pypi|pyproject.toml",
		"pypi|pipfile",
		"pypi|pipfile.lock",
		"pypi|poetry.lock",
	}
	for _, key := range requiredCovered {
		ecosystem, file, _ := strings.Cut(key, "|")
		entry, ok := DependencyCoverageByFile(file)
		if !ok || entry.Ecosystem != ecosystem {
			t.Fatalf("expected covered entry for %q in matrix", key)
		}
		if entry.Status != DependencyCoverageCovered {
			t.Fatalf("entry %q must remain Covered to preserve existing reducer truth (got %q)", key, entry.Status)
		}
	}
	if entry, ok := DependencyCoverageByFile("worker.csproj"); !ok ||
		entry.Ecosystem != "nuget" ||
		entry.Status != DependencyCoverageCovered {
		t.Fatalf("worker.csproj wildcard lookup = %#v, %v; want covered NuGet project entry", entry, ok)
	}

	requiredGaps := []string{
		"go.sum",
	}
	for _, file := range requiredGaps {
		entry, ok := DependencyCoverageByFile(file)
		if !ok {
			t.Fatalf("expected explicit gap entry for %q so missing dependency evidence stays visible", file)
		}
		if entry.Status != DependencyCoverageGap {
			t.Fatalf("entry %q must remain a Gap until a parser fixture proves the upgrade (got %q)", file, entry.Status)
		}
	}
}

// TestDependencyCoverageCoveredJSONFilesEmitDependencyRows binds every Covered
// matrix entry that this JSON package owns to a runtime fixture so the
// documented capability is provable instead of asserted. Non-JSON parsers
// (gomod, future TOML/XML, etc.) are exercised by the parent-level
// TestDependencyCoverageCoveredFilesEmitDependencyRowsThroughEngine that uses
// the parser engine to dispatch the right adapter per ecosystem.
func TestDependencyCoverageCoveredJSONFilesEmitDependencyRows(t *testing.T) {
	t.Parallel()

	fixtures := map[string]coveredFixture{
		"package.json": {
			parser: parseJSONFixture,
			body: `{
  "name": "demo",
  "dependencies": {"lodash": "^4.17.21"},
  "devDependencies": {"vitest": "^2.0.0"}
}`,
			expectedDependencies:  map[string]string{"lodash": "^4.17.21", "vitest": "^2.0.0"},
			expectedPackageMgr:    "npm",
			expectedSection:       "dependencies",
			expectScopeSplit:      true,
			expectedDevDependency: "vitest",
		},
		"package-lock.json": {
			parser: parseJSONFixture,
			body: `{
  "name": "demo",
  "lockfileVersion": 3,
  "packages": {
    "": {"dependencies": {"lodash": "^4.17.21"}},
    "node_modules/lodash": {"version": "4.17.21"}
  }
}`,
			expectedDependencies: map[string]string{"lodash": "4.17.21"},
			expectedPackageMgr:   "npm",
			expectedSection:      "package-lock",
			transitiveBody: `{
  "lockfileVersion": 3,
  "packages": {
    "": {"dependencies": {"vite": "^5.0.0"}},
    "node_modules/vite": {"version": "5.0.0", "dependencies": {"rollup": "^4.0.0"}},
    "node_modules/rollup": {"version": "4.0.0"}
  }
}`,
		},
		"composer.json": {
			parser: parseJSONFixture,
			body: `{
  "name": "demo/app",
  "require": {"monolog/monolog": "^2.0"},
  "require-dev": {"phpunit/phpunit": "^9.0"}
}`,
			expectedDependencies:  map[string]string{"monolog/monolog": "^2.0", "phpunit/phpunit": "^9.0"},
			expectedPackageMgr:    "composer",
			expectedSection:       "require",
			expectScopeSplit:      true,
			expectedDevDependency: "phpunit/phpunit",
		},
		"composer.lock": {
			parser: parseJSONFixture,
			body: `{
  "packages": [
    {"name": "monolog/monolog", "version": "2.9.1"}
  ],
  "packages-dev": [
    {"name": "phpunit/phpunit", "version": "9.6.13"}
  ]
}`,
			expectedDependencies:  map[string]string{"monolog/monolog": "2.9.1", "phpunit/phpunit": "9.6.13"},
			expectedPackageMgr:    "composer",
			expectedSection:       "packages",
			expectScopeSplit:      true,
			expectedDevDependency: "phpunit/phpunit",
		},
		"gemfile": {
			parser: parseRubyFixture,
			body: `source "https://rubygems.org"
gem "rails", "~> 7.1"
group :development, :test do
  gem "rspec-rails", "~> 6.1"
end
`,
			expectedDependencies:  map[string]string{"rails": "~> 7.1", "rspec-rails": "~> 6.1"},
			expectedPackageMgr:    "rubygems",
			expectedSection:       "default",
			expectScopeSplit:      true,
			expectedDevDependency: "rspec-rails",
		},
		"gemfile.lock": {
			parser: parseRubyFixture,
			body: `GEM
  remote: https://rubygems.org/
  specs:
    rails (7.1.3)
      rack (>= 2.2.4)
    rack (2.2.8)

DEPENDENCIES
  rails (~> 7.1)
`,
			expectedDependencies: map[string]string{"rails": "7.1.3", "rack": "2.2.8"},
			expectedPackageMgr:   "rubygems",
			expectedSection:      "gemfile.lock",
			transitiveBody: `GEM
  remote: https://rubygems.org/
  specs:
    rails (7.1.3)
      rack (>= 2.2.4)
    rack (2.2.8)

DEPENDENCIES
  rails (~> 7.1)
`,
		},
		"packages.lock.json": {
			parser: parseJSONFixture,
			body: `{
  "version": 1,
  "dependencies": {
    "net8.0": {
      "Newtonsoft.Json": {
        "type": "Direct",
        "requested": "[13.0.3, )",
        "resolved": "13.0.3"
      }
    }
  }
}`,
			expectedDependencies: map[string]string{"Newtonsoft.Json": "13.0.3"},
			expectedPackageMgr:   "nuget",
			expectedSection:      "packages.lock.json:net8.0",
			transitiveBody: `{
  "version": 1,
  "dependencies": {
    "net8.0": {
      "Newtonsoft.Json": {
        "type": "Direct",
        "requested": "[13.0.3, )",
        "resolved": "13.0.3",
        "dependencies": {
          "System.Text.Encodings.Web": "[8.0.0, )"
        }
      },
      "System.Text.Encodings.Web": {
        "type": "Transitive",
        "resolved": "8.0.0"
      }
    }
  }
}`,
		},
		"Package.resolved": {
			parser: parseJSONFixture,
			body: `{
  "originHash": "example",
  "pins": [
    {
      "identity": "swift-argument-parser",
      "kind": "remoteSourceControl",
      "location": "https://github.com/apple/swift-argument-parser.git",
      "state": {
        "revision": "0123456789abcdef0123456789abcdef01234567",
        "version": "1.2.3"
      }
    }
  ],
  "version": 2
}`,
			expectedDependencies: map[string]string{"github.com/apple/swift-argument-parser": "1.2.3"},
			expectedPackageMgr:   "swift",
			expectedSection:      "Package.resolved",
		},
		"pom.xml": {
			parser: parseMavenFixture,
			body: `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>demo</artifactId>
  <version>1.0.0</version>
  <properties>
    <spring.version>5.3.20</spring.version>
  </properties>
  <dependencies>
    <dependency>
      <groupId>org.springframework</groupId>
      <artifactId>spring-core</artifactId>
      <version>${spring.version}</version>
    </dependency>
    <dependency>
      <groupId>junit</groupId>
      <artifactId>junit</artifactId>
      <version>4.13.2</version>
      <scope>test</scope>
    </dependency>
  </dependencies>
</project>`,
			expectedDependencies: map[string]string{
				"org.springframework:spring-core": "5.3.20",
				"junit:junit":                     "4.13.2",
			},
			expectedPackageMgr:    "maven",
			expectedSection:       "dependencies",
			expectScopeSplit:      true,
			expectedDevDependency: "junit:junit",
		},
		"build.gradle": {
			parser: parseGradleFixture,
			body: `plugins {
    id 'java'
}

dependencies {
    implementation 'org.springframework:spring-core:5.3.20'
    testImplementation 'junit:junit:4.13.2'
}`,
			expectedDependencies: map[string]string{
				"org.springframework:spring-core": "5.3.20",
				"junit:junit":                     "4.13.2",
			},
			expectedPackageMgr:    "gradle",
			expectedSection:       "implementation",
			expectScopeSplit:      true,
			expectedDevDependency: "junit:junit",
		},
		"build.gradle.kts": {
			parser: parseGradleFixture,
			body: `plugins {
    java
}

dependencies {
    implementation("org.springframework:spring-core:5.3.20")
    testImplementation("junit:junit:4.13.2")
}`,
			expectedDependencies: map[string]string{
				"org.springframework:spring-core": "5.3.20",
				"junit:junit":                     "4.13.2",
			},
			expectedPackageMgr:    "gradle",
			expectedSection:       "implementation",
			expectScopeSplit:      true,
			expectedDevDependency: "junit:junit",
		},
		"pipfile.lock": {
			parser: parseJSONFixture,
			body: `{
  "_meta": {"sources": [{"name": "pypi"}]},
  "default": {"requests": {"version": "==2.31.0"}},
  "develop": {"pytest": {"version": "==7.4.4"}}
}`,
			expectedDependencies:  map[string]string{"requests": "2.31.0", "pytest": "7.4.4"},
			expectedPackageMgr:    "pypi",
			expectedSection:       "default",
			expectScopeSplit:      true,
			expectedDevDependency: "pytest",
		},
		"yarn.lock": {
			parser: parseNodeLockfileFixture,
			body: `# yarn lockfile v1

lodash@^4.17.21:
  version "4.17.21"
  resolved "https://registry.yarnpkg.com/lodash/-/lodash-4.17.21.tgz"

vite@^5.0.0:
  version "5.0.0"
  dependencies:
    rollup "^4.0.0"

rollup@^4.0.0:
  version "4.0.0"
`,
			expectedDependencies: map[string]string{"lodash": "4.17.21", "vite": "5.0.0", "rollup": "4.0.0"},
			expectedPackageMgr:   "npm",
			expectedSection:      "yarn.lock",
		},
		"pnpm-lock.yaml": {
			parser: parseNodeLockfileFixture,
			body: `lockfileVersion: '6.0'

importers:
  .:
    dependencies:
      lodash:
        specifier: ^4.17.21
        version: 4.17.21
    devDependencies:
      vitest:
        specifier: ^2.0.0
        version: 2.0.0

packages:

  /lodash@4.17.21:
    resolution: {integrity: sha512-AbCdEf==}
  /vitest@2.0.0:
    resolution: {integrity: sha512-Vit==}
    dependencies:
      vite: 5.0.0
  /vite@5.0.0:
    resolution: {integrity: sha512-V==}
`,
			expectedDependencies:  map[string]string{"lodash": "4.17.21", "vitest": "2.0.0", "vite": "5.0.0"},
			expectedPackageMgr:    "npm",
			expectedSection:       "runtime",
			expectScopeSplit:      true,
			expectedDevDependency: "vitest",
		},
	}

	// Files covered by parsers in sibling packages (pythondep TOML/text
	// parsers) cannot be exercised through json.Parse. They are validated by
	// their own package-level tests; the matrix invariant
	// TestDependencyCoverageMatrix guarantees they remain in the matrix, and
	// the engine matrix test in
	// go/internal/parser/dependency_coverage_engine_test.go dispatches them
	// through the registry the collector uses.
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
			// Non-JSON and wildcard entries are covered by parent-parser tests.
			// The JSON package owns the matrix but cannot import the parent
			// parser without creating an import cycle.
			continue
		}
		if nonJSONCovered[entry.FilePattern] {
			continue
		}
		fixture, ok := fixtures[entry.FilePattern]
		if !ok {
			// Non-JSON covered entries are validated by the parent-level
			// engine test; skip them here so this package keeps its
			// JSON-ownership boundary.
			continue
		}
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
		if fixture.expectScopeSplit {
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
		if entry.CapturesDependencyChain {
			anyChain := false
			for _, row := range rows {
				if path, ok := row["dependency_path"].([]string); ok && len(path) > 1 {
					anyChain = true
					break
				}
			}
			if !anyChain && len(fixture.expectedDependencies) > 0 && fixture.transitiveBody != "" {
				// The base fixture for this covered entry may have only a
				// single direct dep so chain length is one. Re-run with a
				// fixture carrying a transitive edge so the matrix claim is
				// provable.
				rerun, err := fixture.parser(t, entry.FilePattern, fixture.transitiveBody)
				if err != nil {
					t.Fatalf("%s: transitive Parse error = %v", entry.FilePattern, err)
				}
				rerunRows, _ := rerun["variables"].([]map[string]any)
				for _, row := range rerunRows {
					if path, ok := row["dependency_path"].([]string); ok && len(path) > 1 {
						anyChain = true
						break
					}
				}
			}
			if !anyChain {
				t.Fatalf("%s: matrix claims dependency chain capture but parser produced no dependency_path", entry.FilePattern)
			}
		}
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

// parseNodeLockfileFixture routes yarn.lock and pnpm-lock.yaml fixtures
// through the nodelockfile parser. The JSON package cannot import the
// parent parser without an import cycle, so each ecosystem gets a focused
// helper that calls its adapter directly.
func parseNodeLockfileFixture(t *testing.T, filename, body string) (map[string]any, error) {
	t.Helper()
	path := writeJSONTestFile(t, filename, body)
	return nodelockfile.Parse(path, false, shared.Options{})
}

// TestDependencyCoverageGapsDoNotEmitDependencyRows enforces the safety rule
// from issue #571: until a real parser exists, gap files MUST NOT smuggle
// content_entity dependency rows through Parse. Without this gate a partially
// implemented parser could create the illusion of coverage and let the
// reducer admit consumption decisions from unproven evidence.
func TestDependencyCoverageGapsDoNotEmitDependencyRows(t *testing.T) {
	t.Parallel()

	// All JSON-shaped dependency files (composer.lock, packages.lock.json,
	// Pipfile.lock) now have lockfile-aware parsers that emit dependency
	// rows, so this map is intentionally empty. Whenever a JSON gap entry is
	// added to the coverage matrix, add a fixture here so this guard proves
	// the gap parser does not smuggle dependency rows into the fact store.
	jsonGapFixtures := map[string]string{}

	for file, body := range jsonGapFixtures {
		entry, ok := DependencyCoverageByFile(file)
		if !ok {
			t.Fatalf("matrix is missing gap entry for %q", file)
		}
		if entry.Status != DependencyCoverageGap {
			t.Fatalf("%s: expected gap status but matrix says %q", file, entry.Status)
		}
		path := writeJSONTestFile(t, file, body)
		payload, err := Parse(path, false, shared.Options{}, Config{})
		if err != nil {
			t.Fatalf("%s: Parse() error = %v", file, err)
		}
		rows, _ := payload["variables"].([]map[string]any)
		for _, row := range rows {
			if row["config_kind"] == "dependency" {
				t.Fatalf("%s: gap file emitted dependency row %#v; missing evidence would be treated as affected", file, row)
			}
		}
	}
}
