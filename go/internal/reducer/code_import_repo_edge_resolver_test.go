// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestNormalizeImportSourceNPM verifies that npm bare specifiers are normalized
// into (ecosystem, coordinate) pairs and relative/intra-repo paths are dropped.
func TestNormalizeImportSourceNPM(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		source    string
		language  string
		wantEco   string
		wantCoord string
	}{
		{
			name:      "bare package",
			source:    "express",
			language:  "javascript",
			wantEco:   "npm",
			wantCoord: "express",
		},
		{
			name:      "scoped package",
			source:    "@aws-sdk/client-s3",
			language:  "typescript",
			wantEco:   "npm",
			wantCoord: "@aws-sdk/client-s3",
		},
		{
			name:      "relative path dropped",
			source:    "./utils",
			language:  "javascript",
			wantEco:   "",
			wantCoord: "",
		},
		{
			name:      "parent relative path dropped",
			source:    "../common",
			language:  "typescript",
			wantEco:   "",
			wantCoord: "",
		},
		{
			name:      "bare subpath reduced to package root",
			source:    "lodash/fp",
			language:  "javascript",
			wantEco:   "npm",
			wantCoord: "lodash",
		},
		{
			name:      "scoped subpath reduced to scoped package root",
			source:    "@aws-sdk/client-s3/dist",
			language:  "typescript",
			wantEco:   "npm",
			wantCoord: "@aws-sdk/client-s3",
		},
		{
			name:      "intra-repo baseUrl path reduced to first segment (owner lookup drops it)",
			source:    "src/components/Button",
			language:  "javascript",
			wantEco:   "npm",
			wantCoord: "src",
		},
		{
			name:      "node builtin protocol dropped",
			source:    "node:fs",
			language:  "javascript",
			wantEco:   "",
			wantCoord: "",
		},
		{
			name:      "empty source dropped",
			source:    "",
			language:  "javascript",
			wantEco:   "",
			wantCoord: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			eco, coord := normalizeImportSource(tc.source, tc.language)
			if eco != tc.wantEco {
				t.Errorf("ecosystem = %q, want %q", eco, tc.wantEco)
			}
			if coord != tc.wantCoord {
				t.Errorf("coordinate = %q, want %q", coord, tc.wantCoord)
			}
		})
	}
}

// TestNormalizeImportSourcePyPI verifies that Python module import sources are
// normalized to PyPI top-level distribution names.
func TestNormalizeImportSourcePyPI(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		source    string
		language  string
		wantEco   string
		wantCoord string
	}{
		{
			name:      "top-level module",
			source:    "requests",
			language:  "python",
			wantEco:   "pypi",
			wantCoord: "requests",
		},
		{
			name:      "subpackage normalized to top-level",
			source:    "django.core.management",
			language:  "python",
			wantEco:   "pypi",
			wantCoord: "django",
		},
		{
			name:      "relative import dropped (dot prefix)",
			source:    "./module_b",
			language:  "python",
			wantEco:   "",
			wantCoord: "",
		},
		{
			name:      "explicit intra-repo relative dropped",
			source:    ".sibling",
			language:  "python",
			wantEco:   "",
			wantCoord: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			eco, coord := normalizeImportSource(tc.source, tc.language)
			if eco != tc.wantEco {
				t.Errorf("ecosystem = %q, want %q", eco, tc.wantEco)
			}
			if coord != tc.wantCoord {
				t.Errorf("coordinate = %q, want %q", coord, tc.wantCoord)
			}
		})
	}
}

// TestNormalizeImportSourceGoModule verifies that Go import paths are mapped to
// module coordinates. Stdlib packages (no dot in first path segment) are dropped.
func TestNormalizeImportSourceGoModule(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		source    string
		language  string
		wantEco   string
		wantCoord string
	}{
		{
			name:      "full module path kept",
			source:    "github.com/some-org/some-repo",
			language:  "go",
			wantEco:   "gomod",
			wantCoord: "github.com/some-org/some-repo",
		},
		{
			name:      "subpackage path kept as module coordinate",
			source:    "github.com/some-org/some-repo/internal/foo",
			language:  "go",
			wantEco:   "gomod",
			wantCoord: "github.com/some-org/some-repo/internal/foo",
		},
		{
			name:      "stdlib dropped (no dot in host)",
			source:    "fmt",
			language:  "go",
			wantEco:   "",
			wantCoord: "",
		},
		{
			name:      "stdlib multi-segment dropped",
			source:    "net/http",
			language:  "go",
			wantEco:   "",
			wantCoord: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			eco, coord := normalizeImportSource(tc.source, tc.language)
			if eco != tc.wantEco {
				t.Errorf("ecosystem = %q, want %q", eco, tc.wantEco)
			}
			if coord != tc.wantCoord {
				t.Errorf("coordinate = %q, want %q", coord, tc.wantCoord)
			}
		})
	}
}

// TestMatchImportCoordinateToPackageID verifies the alias matcher resolves
// import coordinates to package IDs tolerant of vanity paths, monorepo
// subpaths, and version suffixes.
func TestMatchImportCoordinateToPackageID(t *testing.T) {
	t.Parallel()

	// owners resolves (ecosystem, name) coordinates to an owning repository,
	// built through the sanctioned packageConsumptionKeys join.
	owners := newCodeImportOwnerIndexForTest(map[ecoName]string{
		{"npm", "express"}:                    "repo-express",
		{"gomod", "github.com/gin-gonic/gin"}: "repo-gin",
		{"pypi", "requests"}:                  "repo-requests",
		// monorepo: @aws-sdk packages all owned by same repo
		{"npm", "@aws-sdk/client-s3"}: "repo-aws-sdk",
	})

	cases := []struct {
		name       string
		ecosystem  string
		coordinate string
		wantRepoID string
	}{
		{
			name:       "exact npm match",
			ecosystem:  "npm",
			coordinate: "express",
			wantRepoID: "repo-express",
		},
		{
			name:       "exact go module match",
			ecosystem:  "gomod",
			coordinate: "github.com/gin-gonic/gin",
			wantRepoID: "repo-gin",
		},
		{
			name:       "exact pypi match",
			ecosystem:  "pypi",
			coordinate: "requests",
			wantRepoID: "repo-requests",
		},
		{
			name:       "go module subpath resolves to module root",
			ecosystem:  "gomod",
			coordinate: "github.com/gin-gonic/gin/render",
			wantRepoID: "repo-gin",
		},
		{
			name:       "go module version suffix resolves to base",
			ecosystem:  "gomod",
			coordinate: "github.com/gin-gonic/gin/v2",
			wantRepoID: "repo-gin",
		},
		{
			name:       "scoped npm match",
			ecosystem:  "npm",
			coordinate: "@aws-sdk/client-s3",
			wantRepoID: "repo-aws-sdk",
		},
		{
			name:       "no match returns empty",
			ecosystem:  "npm",
			coordinate: "unknown-pkg",
			wantRepoID: "",
		},
		{
			name:       "empty coordinate returns empty",
			ecosystem:  "npm",
			coordinate: "",
			wantRepoID: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := matchImportCoordinateToOwner(tc.ecosystem, tc.coordinate, owners)
			if got != tc.wantRepoID {
				t.Errorf("matchImportCoordinateToOwner(%q, %q) = %q, want %q",
					tc.ecosystem, tc.coordinate, got, tc.wantRepoID)
			}
		})
	}
}

// TestMatchImportCoordinateToOwnerAmbiguousSkipped verifies that a coordinate
// whose (ecosystem, name) key resolves to more than one owning repository is
// dropped, never guessed.
func TestMatchImportCoordinateToOwnerAmbiguousSkipped(t *testing.T) {
	t.Parallel()

	owners := newAmbiguousCodeImportOwnerIndexForTest("npm", "leftpad", "repo-a", "repo-b")
	if got := matchImportCoordinateToOwner("npm", "leftpad", owners); got != "" {
		t.Errorf("ambiguous coordinate resolved to %q, want \"\"", got)
	}
}

// ecoName is an (ecosystem, name) pair used to seed a test owner index through
// the same packageConsumptionKeys join the production builder uses.
type ecoName struct {
	ecosystem string
	name      string
}

// newCodeImportOwnerIndexForTest builds a codeImportOwnerIndex from
// (ecosystem, name) -> owner repository pairs using the real consumption keys.
func newCodeImportOwnerIndexForTest(pairs map[ecoName]string) codeImportOwnerIndex {
	byKey := make(map[string]string)
	for pair, repoID := range pairs {
		for _, key := range packageConsumptionKeys(pair.ecosystem, pair.name) {
			byKey[key] = repoID
		}
	}
	return codeImportOwnerIndex{byKey: byKey, ambiguous: map[string]struct{}{}}
}

// newAmbiguousCodeImportOwnerIndexForTest builds an index where one coordinate
// is recorded as ambiguous between two owners.
func newAmbiguousCodeImportOwnerIndexForTest(ecosystem, name, repoA, repoB string) codeImportOwnerIndex {
	_ = repoA
	_ = repoB
	ambiguous := make(map[string]struct{})
	for _, key := range packageConsumptionKeys(ecosystem, name) {
		ambiguous[key] = struct{}{}
	}
	// Seed byKey with one unrelated entry so empty() stays false.
	byKey := map[string]string{"npm\x00sentinel": "repo-sentinel"}
	return codeImportOwnerIndex{byKey: byKey, ambiguous: ambiguous}
}

// TestCodeImportEntrySourceRepoRelativeResolvedSourceDropped proves that when
// resolved_source is a repo-relative baseUrl path (e.g. "src/components/Button"
// from tsconfig baseUrl/paths resolution), codeImportEntrySource drops the
// import rather than falling back to the raw source field, so
// normalizeNPMImportSource cannot reduce either value to a bare segment like
// "src" and fabricate a DEPENDS_ON edge against a package named "src" (issue
// #3651, P2).
func TestCodeImportEntrySourceRepoRelativeResolvedSourceDropped(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		resolved   string
		source     string
		wantResult string
	}{
		{
			name:       "baseUrl path src/components/Button dropped (source is a bare alias)",
			resolved:   "src/components/Button",
			source:     "components/Button",
			wantResult: "",
		},
		{
			name:       "baseUrl path app/utils/helper dropped (source is a bare alias)",
			resolved:   "app/utils/helper",
			source:     "utils/helper",
			wantResult: "",
		},
		{
			// Regression for #3651/#3658 review: the raw source can itself be a
			// repo-local alias containing a "/" (parser emits source="resources/jwt",
			// resolved_source="src/resources/jwt.ts"). Falling back to it would let
			// normalizeNPMImportSource reduce it to "resources" and fabricate an edge,
			// so the import must be dropped once resolution proves it intra-repo.
			name:       "baseUrl alias resources/jwt dropped, not reduced to resources",
			resolved:   "src/resources/jwt.ts",
			source:     "resources/jwt",
			wantResult: "",
		},
		{
			name:       "external npm package resolved_source kept",
			resolved:   "express",
			source:     "express",
			wantResult: "express",
		},
		{
			name:       "relative resolved_source kept (normalizer drops it)",
			resolved:   "./local",
			source:     "./local",
			wantResult: "./local",
		},
		{
			name:       "scoped package @scope/name kept",
			resolved:   "@scope/name",
			source:     "@scope/name",
			wantResult: "@scope/name",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			entry := map[string]any{
				"resolved_source": tc.resolved,
				"source":          tc.source,
			}
			got := codeImportEntrySource(entry)
			if got != tc.wantResult {
				t.Errorf("codeImportEntrySource() = %q, want %q", got, tc.wantResult)
			}
		})
	}
}

// TestBuildCodeImportRepoDependencyIntentsTsConfigBaseUrlDropped proves that a
// TypeScript import whose resolved_source is a repo-relative baseUrl path
// (e.g. "src/components/Button") does NOT produce a DEPENDS_ON edge even when
// the package catalog has an owned package whose name matches the first segment
// of that path (e.g. "src" owned by another repo). Without the fix,
// normalizeNPMImportSource reduces "src/components/Button" to "src", the owner
// lookup succeeds, and a fabricated cross-repo edge is emitted (issue #3651, P2).
func TestBuildCodeImportRepoDependencyIntentsTsConfigBaseUrlDropped(t *testing.T) {
	t.Parallel()

	// Index maps bare name "src" to a real repo — the scenario that triggers fabrication.
	owners := newCodeImportOwnerIndexForTest(map[ecoName]string{
		{"npm", "src"}: "repo-src-pkg",
	})

	imports := []map[string]any{
		{
			"resolved_source": "src/components/Button", // tsconfig baseUrl-resolved repo-relative path
			"source":          "components/Button",
		},
	}
	envelope := facts.Envelope{
		FactKind: factKindFile,
		Payload: map[string]any{
			"repo_id":          "consumer-repo",
			"relative_path":    "src/App.tsx",
			"language":         "typescript",
			"parsed_file_data": map[string]any{"imports": imports},
		},
	}

	input := CodeImportRepoDependencyInput{
		ScopeID:       "scope-baseurl",
		GenerationID:  "gen-baseurl",
		SourceRunID:   "code_import_repo_dependency:scope-baseurl",
		CreatedAt:     time.Now(),
		FileEnvelopes: []facts.Envelope{envelope},
		Owners:        owners,
	}

	intents := BuildCodeImportRepoDependencyIntents(input)
	if len(intents) != 0 {
		t.Fatalf(
			"BuildCodeImportRepoDependencyIntents() = %d intents, want 0 (repo-relative resolved_source must not fabricate edge); target_repo_id=%v",
			len(intents),
			intents[0].Payload["target_repo_id"],
		)
	}
}
