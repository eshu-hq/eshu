// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"testing"
)

type vulnScanFixtureCorpusCase struct {
	name           string
	fixture        string
	manager        string
	ecosystem      string
	state          string
	expectedFiles  []string
	dependencyPath []any
	direct         bool
	dev            bool
}

func TestVulnScanRepoFixtureCorpusCoversSupportedManagers(t *testing.T) {
	cases := vulnScanFixtureCorpusCases()
	coverage := map[string]map[string]bool{}
	for _, tc := range cases {
		if tc.fixture == "" {
			t.Fatalf("fixture case %q must declare fixture directory", tc.name)
		}
		if tc.manager == "" {
			t.Fatalf("fixture case %q must declare package manager", tc.name)
		}
		if tc.ecosystem == "" {
			t.Fatalf("fixture case %q must declare ecosystem", tc.name)
		}
		state := coverage[tc.manager]
		if state == nil {
			state = map[string]bool{}
			coverage[tc.manager] = state
		}
		state[tc.state] = true
		for _, rel := range tc.expectedFiles {
			path := filepath.Join("testdata", "vuln_scan_repo_fixtures", tc.fixture, rel)
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("fixture %q missing required file %q: %v", tc.fixture, rel, err)
			}
		}
	}

	requiredManagers := []string{
		"npm",
		"yarn",
		"pnpm",
		"go",
		"pypi-requirements",
		"pypi-pyproject",
		"pypi-pipfile",
		"pypi-poetry",
		"maven",
		"gradle",
		"composer",
		"bundler",
		"cargo",
		"nuget",
		"apk",
		"dpkg",
		"rpm",
	}
	for _, manager := range requiredManagers {
		state := coverage[manager]
		if !state["vulnerable"] || !state["ready_zero"] {
			t.Fatalf("manager %q coverage = %#v, want vulnerable and ready_zero local fixtures", manager, state)
		}
	}
	if !coverage["yarn"]["malformed"] {
		t.Fatalf("yarn coverage = %#v, want malformed fail-closed fixture", coverage["yarn"])
	}
	if !coverage["pub"]["unsupported"] {
		t.Fatalf("pub coverage = %#v, want unsupported fixture", coverage["pub"])
	}
	if !coverage["pypi-requirements"]["missing_evidence"] {
		t.Fatalf("pypi-requirements coverage = %#v, want missing-evidence fixture", coverage["pypi-requirements"])
	}
}

func TestRunVulnScanRepoFixtureCorpusUsesReadinessEnvelopeSemantics(t *testing.T) {
	for _, tc := range vulnScanFixtureCorpusCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			matrixCase := vulnScanFixtureCorpusMatrixCase(tc)
			out, err := runVulnScanFixtureMatrixCase(t, matrixCase, true)
			requireVulnScanExitCode(t, err, matrixCase.exitCode)
			payload := decodeVulnScanPayload(t, out)
			data := payload["data"].(map[string]any)
			if got, want := data["repository_id"], matrixCase.repositoryID; got != want {
				t.Fatalf("data[repository_id] = %#v, want %#v", got, want)
			}
			if got := data["readiness_state"]; got != expectedFixtureCorpusReadinessState(matrixCase) {
				t.Fatalf("data[readiness_state] = %#v, want %q", got, expectedFixtureCorpusReadinessState(matrixCase))
			}
			report := requireMapField(t, data, "report")
			summary := requireMapField(t, report, "summary")
			if got := toInt(t, summary["exit_code"]); got != matrixCase.exitCode {
				t.Fatalf("summary[exit_code] = %d, want %d", got, matrixCase.exitCode)
			}
		})
	}
}

func vulnScanFixtureCorpusCases() []vulnScanFixtureCorpusCase {
	return []vulnScanFixtureCorpusCase{
		{
			name:           "npm vulnerable lockfile exact",
			fixture:        "vulnerable-npm",
			manager:        "npm",
			ecosystem:      "npm",
			state:          "vulnerable",
			expectedFiles:  []string{"package.json", "package-lock.json"},
			dependencyPath: []any{"synthetic-root", "synthetic-vulnerable-npm"},
			direct:         true,
		},
		{
			name:          "npm ready zero",
			fixture:       "ready-zero-npm",
			manager:       "npm",
			ecosystem:     "npm",
			state:         "ready_zero",
			expectedFiles: []string{"package.json", "package-lock.json"},
			direct:        true,
		},
		{
			name:           "yarn vulnerable transitive",
			fixture:        "vulnerable-yarn",
			manager:        "yarn",
			ecosystem:      "npm",
			state:          "vulnerable",
			expectedFiles:  []string{"package.json", "yarn.lock"},
			dependencyPath: []any{"synthetic-yarn-root", "synthetic-yarn-parent", "synthetic-yarn-transitive"},
		},
		{
			name:          "yarn ready zero",
			fixture:       "ready-zero-yarn",
			manager:       "yarn",
			ecosystem:     "npm",
			state:         "ready_zero",
			expectedFiles: []string{"package.json", "yarn.lock"},
			direct:        true,
		},
		{
			name:          "pnpm vulnerable dev dependency",
			fixture:       "vulnerable-pnpm",
			manager:       "pnpm",
			ecosystem:     "npm",
			state:         "vulnerable",
			expectedFiles: []string{"package.json", "pnpm-lock.yaml"},
			dev:           true,
			direct:        true,
		},
		{
			name:          "pnpm ready zero",
			fixture:       "ready-zero-pnpm",
			manager:       "pnpm",
			ecosystem:     "npm",
			state:         "ready_zero",
			expectedFiles: []string{"package.json", "pnpm-lock.yaml"},
			direct:        true,
		},
		{
			name:          "go vulnerable module",
			fixture:       "vulnerable-go",
			manager:       "go",
			ecosystem:     "go",
			state:         "vulnerable",
			expectedFiles: []string{"go.mod", "go.sum"},
			direct:        true,
		},
		{
			name:          "go ready zero",
			fixture:       "ready-zero-go",
			manager:       "go",
			ecosystem:     "go",
			state:         "ready_zero",
			expectedFiles: []string{"go.mod", "go.sum"},
			direct:        true,
		},
		{
			name:          "pypi requirements vulnerable",
			fixture:       "vulnerable-pypi-requirements",
			manager:       "pypi-requirements",
			ecosystem:     "pypi",
			state:         "vulnerable",
			expectedFiles: []string{"requirements.txt", "requirements-dev.txt"},
			dev:           true,
			direct:        true,
		},
		{
			name:          "pypi requirements ready zero",
			fixture:       "ready-zero-pypi-requirements",
			manager:       "pypi-requirements",
			ecosystem:     "pypi",
			state:         "ready_zero",
			expectedFiles: []string{"requirements.txt"},
			direct:        true,
		},
		{
			name:          "pypi pyproject vulnerable",
			fixture:       "vulnerable-pypi-pyproject",
			manager:       "pypi-pyproject",
			ecosystem:     "pypi",
			state:         "vulnerable",
			expectedFiles: []string{"pyproject.toml"},
			direct:        true,
		},
		{
			name:          "pypi pyproject ready zero",
			fixture:       "ready-zero-pypi-pyproject",
			manager:       "pypi-pyproject",
			ecosystem:     "pypi",
			state:         "ready_zero",
			expectedFiles: []string{"pyproject.toml"},
			direct:        true,
		},
		{
			name:          "pypi pipfile vulnerable",
			fixture:       "vulnerable-pypi-pipfile",
			manager:       "pypi-pipfile",
			ecosystem:     "pypi",
			state:         "vulnerable",
			expectedFiles: []string{"Pipfile", "Pipfile.lock"},
			direct:        true,
		},
		{
			name:          "pypi pipfile ready zero",
			fixture:       "ready-zero-pypi-pipfile",
			manager:       "pypi-pipfile",
			ecosystem:     "pypi",
			state:         "ready_zero",
			expectedFiles: []string{"Pipfile", "Pipfile.lock"},
			direct:        true,
		},
		{
			name:          "pypi poetry vulnerable",
			fixture:       "vulnerable-pypi-poetry",
			manager:       "pypi-poetry",
			ecosystem:     "pypi",
			state:         "vulnerable",
			expectedFiles: []string{"pyproject.toml", "poetry.lock"},
			direct:        true,
		},
		{
			name:          "pypi poetry ready zero",
			fixture:       "ready-zero-pypi-poetry",
			manager:       "pypi-poetry",
			ecosystem:     "pypi",
			state:         "ready_zero",
			expectedFiles: []string{"pyproject.toml", "poetry.lock"},
			direct:        true,
		},
		{
			name:          "maven vulnerable",
			fixture:       "vulnerable-maven",
			manager:       "maven",
			ecosystem:     "maven",
			state:         "vulnerable",
			expectedFiles: []string{"pom.xml"},
			direct:        true,
		},
		{
			name:          "maven ready zero",
			fixture:       "ready-zero-maven",
			manager:       "maven",
			ecosystem:     "maven",
			state:         "ready_zero",
			expectedFiles: []string{"pom.xml"},
			direct:        true,
		},
		{
			name:          "gradle vulnerable",
			fixture:       "vulnerable-gradle",
			manager:       "gradle",
			ecosystem:     "maven",
			state:         "vulnerable",
			expectedFiles: []string{"build.gradle"},
			direct:        true,
		},
		{
			name:          "gradle ready zero",
			fixture:       "ready-zero-gradle",
			manager:       "gradle",
			ecosystem:     "maven",
			state:         "ready_zero",
			expectedFiles: []string{"build.gradle.kts"},
			direct:        true,
		},
		{
			name:          "composer vulnerable",
			fixture:       "vulnerable-composer",
			manager:       "composer",
			ecosystem:     "composer",
			state:         "vulnerable",
			expectedFiles: []string{"composer.json", "composer.lock"},
			direct:        true,
		},
		{
			name:          "composer ready zero",
			fixture:       "ready-zero-composer",
			manager:       "composer",
			ecosystem:     "composer",
			state:         "ready_zero",
			expectedFiles: []string{"composer.json", "composer.lock"},
			direct:        true,
		},
		{
			name:          "bundler vulnerable",
			fixture:       "vulnerable-bundler",
			manager:       "bundler",
			ecosystem:     "rubygems",
			state:         "vulnerable",
			expectedFiles: []string{"Gemfile", "Gemfile.lock"},
			direct:        true,
		},
		{
			name:          "bundler ready zero",
			fixture:       "ready-zero-bundler",
			manager:       "bundler",
			ecosystem:     "rubygems",
			state:         "ready_zero",
			expectedFiles: []string{"Gemfile", "Gemfile.lock"},
			direct:        true,
		},
		{
			name:          "cargo vulnerable",
			fixture:       "vulnerable-cargo",
			manager:       "cargo",
			ecosystem:     "cargo",
			state:         "vulnerable",
			expectedFiles: []string{"Cargo.toml", "Cargo.lock"},
			direct:        true,
		},
		{
			name:          "cargo ready zero",
			fixture:       "ready-zero-cargo",
			manager:       "cargo",
			ecosystem:     "cargo",
			state:         "ready_zero",
			expectedFiles: []string{"Cargo.toml", "Cargo.lock"},
			direct:        true,
		},
		{
			name:          "nuget vulnerable",
			fixture:       "vulnerable-nuget",
			manager:       "nuget",
			ecosystem:     "nuget",
			state:         "vulnerable",
			expectedFiles: []string{"Worker.csproj", "packages.lock.json"},
			direct:        true,
		},
		{
			name:          "nuget ready zero",
			fixture:       "ready-zero-nuget",
			manager:       "nuget",
			ecosystem:     "nuget",
			state:         "ready_zero",
			expectedFiles: []string{"Worker.csproj", "packages.lock.json"},
			direct:        true,
		},
		{
			name:          "apk vulnerable image fixture",
			fixture:       "vulnerable-apk-rootfs",
			manager:       "apk",
			ecosystem:     "os",
			state:         "vulnerable",
			expectedFiles: []string{"etc/os-release", "lib/apk/db/installed"},
			direct:        true,
		},
		{
			name:          "apk ready zero image fixture",
			fixture:       "ready-zero-apk-rootfs",
			manager:       "apk",
			ecosystem:     "os",
			state:         "ready_zero",
			expectedFiles: []string{"etc/os-release", "lib/apk/db/installed"},
			direct:        true,
		},
		{
			name:          "dpkg vulnerable image fixture",
			fixture:       "vulnerable-dpkg-rootfs",
			manager:       "dpkg",
			ecosystem:     "os",
			state:         "vulnerable",
			expectedFiles: []string{"etc/os-release", "var/lib/dpkg/status"},
			direct:        true,
		},
		{
			name:          "dpkg ready zero image fixture",
			fixture:       "ready-zero-dpkg-rootfs",
			manager:       "dpkg",
			ecosystem:     "os",
			state:         "ready_zero",
			expectedFiles: []string{"etc/os-release", "var/lib/dpkg/status"},
			direct:        true,
		},
		{
			name:          "rpm vulnerable queryformat fixture",
			fixture:       "vulnerable-rpm-rootfs",
			manager:       "rpm",
			ecosystem:     "os",
			state:         "vulnerable",
			expectedFiles: []string{"etc/os-release", "var/lib/rpm/synthetic-queryformat.txt"},
			direct:        true,
		},
		{
			name:          "rpm ready zero queryformat fixture",
			fixture:       "ready-zero-rpm-rootfs",
			manager:       "rpm",
			ecosystem:     "os",
			state:         "ready_zero",
			expectedFiles: []string{"etc/os-release", "var/lib/rpm/synthetic-queryformat.txt"},
			direct:        true,
		},
		{
			name:          "yarn malformed fail closed",
			fixture:       "malformed-yarn",
			manager:       "yarn",
			ecosystem:     "npm",
			state:         "malformed",
			expectedFiles: []string{"package.json", "yarn.lock"},
		},
		{
			name:          "pub unsupported",
			fixture:       "unsupported-pub",
			manager:       "pub",
			ecosystem:     "pub",
			state:         "unsupported",
			expectedFiles: []string{"pubspec.yaml", "pubspec.lock"},
		},
		{
			name:          "pypi requirements missing advisory evidence",
			fixture:       "missing-evidence-pypi-requirements",
			manager:       "pypi-requirements",
			ecosystem:     "pypi",
			state:         "missing_evidence",
			expectedFiles: []string{"requirements.txt"},
		},
	}
}
