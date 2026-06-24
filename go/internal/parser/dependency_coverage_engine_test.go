// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"os"
	"path/filepath"
	"testing"

	jsonparser "github.com/eshu-hq/eshu/go/internal/parser/json"
)

// TestDependencyCoverageCoveredFilesEmitDependencyRowsThroughEngine binds
// every Covered matrix entry to a runtime fixture parsed through the parent
// parser engine so the documented capability is provable for every ecosystem
// regardless of which adapter package owns it. A regression here means we
// shipped a doc claim that no longer matches what the engine actually does.
//
// This is the parent-level companion to
// TestDependencyCoverageCoveredJSONFilesEmitDependencyRows in
// internal/parser/json: the JSON test stays focused on JSON-format covered
// entries and this test covers gomod, pythondep, cargo, ruby, nuget and any
// future non-JSON covered adapters. Every Covered matrix entry MUST have a
// fixture here.
func TestDependencyCoverageCoveredFilesEmitDependencyRowsThroughEngine(t *testing.T) {
	t.Parallel()

	type coverageFixture struct {
		body                 string
		expectedDependencies map[string]string
		expectedPackageMgr   string
		// filenameOverride, when non-empty, is the on-disk filename used in
		// the temp directory. The fixture map is keyed by the matrix
		// FilePattern (for example "*.csproj") so wildcard patterns still
		// map to a single fixture; this field provides the concrete file
		// name the engine sees on disk.
		filenameOverride string
	}
	fixtures := map[string]coverageFixture{
		"package.json": {
			body: `{
  "name": "demo",
  "dependencies": {"lodash": "^4.17.21"},
  "devDependencies": {"vitest": "^2.0.0"}
}`,
			expectedDependencies: map[string]string{"lodash": "^4.17.21", "vitest": "^2.0.0"},
			expectedPackageMgr:   "npm",
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
		},
		"composer.json": {
			body: `{
  "name": "demo/app",
  "require": {"monolog/monolog": "^2.0"}
}`,
			expectedDependencies: map[string]string{"monolog/monolog": "^2.0"},
			expectedPackageMgr:   "composer",
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
			expectedDependencies: map[string]string{
				"monolog/monolog": "2.9.1",
				"phpunit/phpunit": "9.6.13",
			},
			expectedPackageMgr: "composer",
		},
		"go.mod": {
			body: `module example.com/app

go 1.22

require (
	golang.org/x/text v0.3.7
	golang.org/x/sys v0.10.0 // indirect
)
`,
			expectedDependencies: map[string]string{
				"golang.org/x/text": "v0.3.7",
				"golang.org/x/sys":  "v0.10.0",
			},
			expectedPackageMgr: "go",
		},
		"gemfile": {
			body: `source "https://rubygems.org"

gem "rails", "~> 7.1"
`,
			expectedDependencies: map[string]string{"rails": "~> 7.1"},
			expectedPackageMgr:   "rubygems",
		},
		"gemfile.lock": {
			body: `GEM
  remote: https://rubygems.org/
  specs:
    rails (7.1.3)
    puma (6.4.2)

PLATFORMS
  ruby

DEPENDENCIES
  rails (~> 7.1)
  puma

BUNDLED WITH
   2.5.6
`,
			expectedDependencies: map[string]string{
				"rails": "7.1.3",
				"puma":  "6.4.2",
			},
			expectedPackageMgr: "rubygems",
		},
		"*.csproj": {
			body: `<Project Sdk="Microsoft.NET.Sdk">
  <ItemGroup>
    <PackageReference Include="Newtonsoft.Json" Version="[13.0.3]" />
  </ItemGroup>
</Project>`,
			expectedDependencies: map[string]string{"Newtonsoft.Json": "[13.0.3]"},
			expectedPackageMgr:   "nuget",
			filenameOverride:     "Worker.csproj",
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
		},
		"Package.resolved": {
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
		},
		"pubspec.yaml": {
			body: `name: demo
dependencies:
  http: ^1.2.0
dev_dependencies:
  test: any
`,
			expectedDependencies: map[string]string{
				"http": "^1.2.0",
				"test": "any",
			},
			expectedPackageMgr: "pub",
		},
		"pubspec.lock": {
			body: `packages:
  http:
    dependency: "direct main"
    description:
      name: http
      url: "https://pub.dev"
    source: hosted
    version: "1.2.2"
`,
			expectedDependencies: map[string]string{"http": "1.2.2"},
			expectedPackageMgr:   "pub",
		},
		"cargo.toml": {
			body: `[package]
name = "demo"
version = "0.1.0"

[dependencies]
tokio = "1.37"
`,
			expectedDependencies: map[string]string{"tokio": "1.37"},
			expectedPackageMgr:   "cargo",
		},
		"cargo.lock": {
			body: `version = 3

[[package]]
name = "demo"
version = "0.1.0"
dependencies = [
 "tokio",
]

[[package]]
name = "tokio"
version = "1.37.0"
source = "registry+https://github.com/rust-lang/crates.io-index"
`,
			expectedDependencies: map[string]string{"tokio": "1.37.0"},
			expectedPackageMgr:   "cargo",
		},
		"mix.exs": {
			body: `defmodule Demo.MixProject do
  use Mix.Project
  defp deps do
    [
      {:phoenix_html, "~> 4.2"},
      {:jason, "~> 1.4", only: :test}
    ]
  end
end
`,
			expectedDependencies: map[string]string{
				"phoenix_html": "~> 4.2",
				"jason":        "~> 1.4",
			},
			expectedPackageMgr: "hex",
		},
		"mix.lock": {
			body: `%{
  "phoenix_html": {:hex, :phoenix_html, "4.2.1", "checksum", [:mix], [], "hexpm", "outer"},
  "decimal": {:hex, :decimal, "2.1.1", "checksum", [:mix], [], "hexpm", "outer"}
}
`,
			expectedDependencies: map[string]string{
				"phoenix_html": "4.2.1",
				"decimal":      "2.1.1",
			},
			expectedPackageMgr: "hex",
		},
		"pom.xml": {
			body: `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>demo</artifactId>
  <version>1.0.0</version>
  <dependencies>
    <dependency>
      <groupId>org.springframework</groupId>
      <artifactId>spring-core</artifactId>
      <version>5.3.20</version>
    </dependency>
  </dependencies>
</project>`,
			expectedDependencies: map[string]string{"org.springframework:spring-core": "5.3.20"},
			expectedPackageMgr:   "maven",
		},
		"build.gradle": {
			body: `plugins { id 'java' }

dependencies {
    implementation 'org.springframework:spring-core:5.3.20'
}`,
			expectedDependencies: map[string]string{"org.springframework:spring-core": "5.3.20"},
			expectedPackageMgr:   "gradle",
		},
		"build.gradle.kts": {
			body: `plugins { java }

dependencies {
    implementation("org.springframework:spring-core:5.3.20")
}`,
			expectedDependencies: map[string]string{"org.springframework:spring-core": "5.3.20"},
			expectedPackageMgr:   "gradle",
		},
		"requirements.txt": {
			body: "requests==2.31.0\n" +
				"Django>=4.2,<5.0\n",
			expectedDependencies: map[string]string{
				"requests": "==2.31.0",
				"Django":   ">=4.2,<5.0",
			},
			expectedPackageMgr: "pypi",
		},
		"pyproject.toml": {
			body: "[project]\n" +
				"name = \"demo\"\n" +
				"dependencies = [\"requests>=2.0\", \"numpy[mkl]~=1.26\"]\n" +
				"\n" +
				"[tool.poetry.group.dev.dependencies]\n" +
				"pytest = \"^7\"\n",
			expectedDependencies: map[string]string{
				"requests": ">=2.0",
				"numpy":    "~=1.26",
				"pytest":   "^7",
			},
			expectedPackageMgr: "pypi",
		},
		"pipfile": {
			body: "[packages]\n" +
				"requests = \"==2.31.0\"\n" +
				"\n" +
				"[dev-packages]\n" +
				"pytest = \">=7.0\"\n",
			expectedDependencies: map[string]string{
				"requests": "==2.31.0",
				"pytest":   ">=7.0",
			},
			expectedPackageMgr: "pypi",
			filenameOverride:   "Pipfile",
		},
		"poetry.lock": {
			body: "[[package]]\n" +
				"name = \"requests\"\n" +
				"version = \"2.31.0\"\n" +
				"\n" +
				"[[package]]\n" +
				"name = \"pytest\"\n" +
				"version = \"7.4.4\"\n" +
				"category = \"dev\"\n",
			expectedDependencies: map[string]string{
				"requests": "2.31.0",
				"pytest":   "7.4.4",
			},
			expectedPackageMgr: "pypi",
		},
		"pipfile.lock": {
			body: `{
  "_meta": {"sources": [{"name": "pypi"}]},
  "default": {"requests": {"version": "==2.31.0"}},
  "develop": {"pytest": {"version": "==7.4.4"}}
}`,
			expectedDependencies: map[string]string{
				"requests": "2.31.0",
				"pytest":   "7.4.4",
			},
			expectedPackageMgr: "pypi",
			filenameOverride:   "Pipfile.lock",
		},
		"yarn.lock": {
			body: `# yarn lockfile v1

lodash@^4.17.21:
  version "4.17.21"
  resolved "https://registry.yarnpkg.com/lodash/-/lodash-4.17.21.tgz"
`,
			expectedDependencies: map[string]string{"lodash": "4.17.21"},
			// Yarn (and pnpm) rows keep the canonical npm ecosystem so
			// reducer SQL filters match them as npm evidence; the actual
			// package manager flavor lives in package_manager_flavor.
			expectedPackageMgr: "npm",
		},
		"pnpm-lock.yaml": {
			body: `lockfileVersion: '6.0'

importers:
  .:
    dependencies:
      lodash:
        specifier: ^4.17.21
        version: 4.17.21

packages:

  /lodash@4.17.21:
    resolution: {integrity: sha512-AbCdEf==}
`,
			expectedDependencies: map[string]string{"lodash": "4.17.21"},
			expectedPackageMgr:   "npm",
		},
	}

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	covered := 0
	for _, entry := range jsonparser.DependencyCoverage() {
		if entry.Status != jsonparser.DependencyCoverageCovered {
			continue
		}
		covered++
		fixture, ok := fixtures[entry.FilePattern]
		if !ok {
			t.Fatalf("covered entry %q has no engine-level fixture; add one so the coverage claim is testable", entry.FilePattern)
		}
		dir := t.TempDir()
		filename := entry.FilePattern
		if fixture.filenameOverride != "" {
			filename = fixture.filenameOverride
		}
		path := filepath.Join(dir, filename)
		if err := os.WriteFile(path, []byte(fixture.body), 0o600); err != nil {
			t.Fatalf("%s: write fixture error = %v", entry.FilePattern, err)
		}
		payload, err := engine.ParsePath(dir, path, false, Options{})
		if err != nil {
			t.Fatalf("%s: ParsePath() error = %v", entry.FilePattern, err)
		}
		rows, _ := payload["variables"].([]map[string]any)
		dependencyRows := dependencyConfigKindRowsByName(rows)
		for wantName, wantValue := range fixture.expectedDependencies {
			row, ok := dependencyRows[wantName]
			if !ok {
				t.Fatalf("%s: dependency %q missing from variables %#v", entry.FilePattern, wantName, rows)
			}
			if row["package_manager"] != fixture.expectedPackageMgr {
				t.Fatalf("%s: %q package_manager = %#v, want %q", entry.FilePattern, wantName, row["package_manager"], fixture.expectedPackageMgr)
			}
			if row["value"] != wantValue {
				t.Fatalf("%s: %q value = %#v, want %q", entry.FilePattern, wantName, row["value"], wantValue)
			}
		}
	}
	if covered == 0 {
		t.Fatalf("no covered matrix entries iterated; coverage matrix likely lost all covered rows")
	}
}

// TestDependencyCoverageGoSumDoesNotEmitConsumptionRows enforces the
// checksum-only ambiguity rule: parsing go.sum through the parent engine
// MUST NOT produce config_kind=dependency rows. go.sum records every module
// version any tool has verified, so on its own it cannot prove which version
// is currently selected. The reducer treats go.sum as missing evidence
// until paired with a go.mod require.
func TestDependencyCoverageGoSumDoesNotEmitConsumptionRows(t *testing.T) {
	t.Parallel()

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "go.sum")
	body := `golang.org/x/text v0.3.7 h1:olpwvP2KacW1ZWvsR7uQhoyTYvKAupfQrRGBFM352Gk=
golang.org/x/text v0.3.7/go.mod h1:5Zf9MlPGSHRzGAY0xqgNYbsmkNibR7P++ZRPSqVbA0Q=
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture error = %v", err)
	}
	payload, err := engine.ParsePath(dir, path, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}
	rows, _ := payload["variables"].([]map[string]any)
	for _, row := range rows {
		if row["config_kind"] == "dependency" {
			t.Fatalf("go.sum emitted a config_kind=dependency row %#v; checksum-only evidence must never be admitted as consumption", row)
		}
	}
}

func dependencyConfigKindRowsByName(rows []map[string]any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		if row["config_kind"] != "dependency" {
			continue
		}
		name, _ := row["name"].(string)
		if name != "" {
			out[name] = row
		}
	}
	return out
}
