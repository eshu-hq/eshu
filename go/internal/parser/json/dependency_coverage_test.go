package json

import (
	"strings"
	"testing"

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
		"nuget|*.csproj",
		"nuget|packages.lock.json",
		"rubygems|gemfile",
		"rubygems|gemfile.lock",
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
		"yarn.lock",
		"pnpm-lock.yaml",
		"pyproject.toml",
		"requirements.txt",
		"pipfile",
		"pipfile.lock",
		"poetry.lock",
		"go.mod",
		"go.sum",
		"pom.xml",
		"build.gradle",
		"build.gradle.kts",
		"cargo.toml",
		"cargo.lock",
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

// TestDependencyCoverageCoveredFilesEmitDependencyRows binds every Covered
// matrix entry to a runtime fixture so the documented capability is provable
// instead of asserted. A regression here means we shipped a doc claim that no
// longer matches what Parse actually does.
func TestDependencyCoverageCoveredFilesEmitDependencyRows(t *testing.T) {
	t.Parallel()

	fixtures := map[string]struct {
		body                  string
		expectedDependencies  map[string]string
		expectedPackageMgr    string
		expectedSection       string
		expectScopeSplit      bool
		expectedDevDependency string
		transitiveBody        string
	}{
		"package.json": {
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
	}

	for _, entry := range DependencyCoverage() {
		if entry.Status != DependencyCoverageCovered {
			continue
		}
		if strings.Contains(entry.FilePattern, "*") {
			// Non-JSON wildcard manifests are covered by parser package tests.
			continue
		}
		fixture, ok := fixtures[entry.FilePattern]
		if !ok {
			t.Fatalf("covered entry %s has no fixture; add one to lock in the parser contract", entry.FilePattern)
		}
		path := writeJSONTestFile(t, entry.FilePattern, fixture.body)
		payload, err := parseDependencyCoverageFixture(path, entry.FilePattern)
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
				if path, ok := row["dependency_path"].([]string); ok && len(path) > 0 {
					anyChain = true
					break
				}
			}
			if !anyChain && len(fixture.expectedDependencies) > 0 {
				// package-lock fixture above has a single direct dep so
				// chain length is one. Re-run a fixture with a transitive
				// edge so the matrix claim is provable.
				transitive := writeJSONTestFile(t, entry.FilePattern, fixture.transitiveBody)
				rerun, err := parseDependencyCoverageFixture(transitive, entry.FilePattern)
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

// TestDependencyCoverageGapsDoNotEmitDependencyRows enforces the safety rule
// from issue #571: until a real parser exists, gap files MUST NOT smuggle
// content_entity dependency rows through Parse. Without this gate a partially
// implemented parser could create the illusion of coverage and let the
// reducer admit consumption decisions from unproven evidence.
func TestDependencyCoverageGapsDoNotEmitDependencyRows(t *testing.T) {
	t.Parallel()

	jsonGapFixtures := map[string]string{
		"pipfile.lock": `{"_meta":{},"default":{"requests":{"version":"==2.31.0"}}}`,
	}

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

func parseDependencyCoverageFixture(path string, filePattern string) (map[string]any, error) {
	switch strings.ToLower(filePattern) {
	case "gemfile", "gemfile.lock":
		return ruby.Parse(path, false, shared.Options{})
	default:
		return Parse(path, false, shared.Options{}, Config{})
	}
}
