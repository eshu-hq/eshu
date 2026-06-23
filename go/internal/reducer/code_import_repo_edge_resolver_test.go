package reducer

import (
	"testing"
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
