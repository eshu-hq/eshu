// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	"strings"
	"testing"

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
		"hex|mix.exs",
		"hex|mix.lock",
		"pub|pubspec.yaml",
		"pub|pubspec.lock",
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
