// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidencecontinuity

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// firstPartyImportPrefix is the import-path prefix every package in this
// repository shares. Trimming it from an import path yields the repo-relative
// directory, which is the form trigger globs are written in.
const firstPartyImportPrefix = "github.com/eshu-hq/eshu/"

// TestValidatorGateTriggerCoverage_CodeDependencyAnchorReported pins the third
// anchor category: the packages this validator is built from.
//
// The other two categories watch data the validator reads. Neither watches its
// code, so a change to cigates.MatchGlob or cigates.DornyFilters -- the
// functions that decide what gate_trigger_gap reports -- selected no trigger at
// all, and the validator's own package is named by no `go test` proof ref, so
// the package half never demanded it either. Dropping either from both trigger
// sets therefore passed every gate: the #6131 blind spot re-opened one layer
// deeper, on a code dependency rather than a data file.
//
// Each subtest covers every other anchor deliberately, so the dropped package
// is the only thing that can produce a finding.
func TestValidatorGateTriggerCoverage_CodeDependencyAnchorReported(t *testing.T) {
	if len(validatorCodeDeps) == 0 {
		t.Fatal("validatorCodeDeps is empty; the code-dependency check would evaluate nothing")
	}
	for _, dep := range validatorCodeDeps {
		t.Run(dep, func(t *testing.T) {
			covered := coveredExcept("go/internal/query/**", dep+"/**")
			root := writeGateTriggerFixture(t, covered, covered)

			findings, err := validateGateTriggerCoverage(root, gateTriggerContract())
			if err != nil {
				t.Fatalf("validateGateTriggerCoverage() error = %v", err)
			}
			mustContainFinding(t, findings, FindingGateTriggerGap, "selects "+dep)
			for _, f := range findings {
				if !strings.Contains(f.Message, dep) {
					t.Fatalf("every other anchor is covered by the fixture yet something else was reported: %s", f.Message)
				}
			}
		})
	}
}

// TestValidatorGateTriggerCoverage_CodeDepGlobMustSpanWholePackage rejects a
// filename-narrowed code-dependency trigger. A glob such as
// "go/internal/cigates/g*.go" matches glob.go, the file MatchGlob lives in, and
// would read as coverage against a single probe -- while leaving every other
// file in the package outside the gate's reach. The compiler builds all of
// them, so partial coverage is a blind spot, which is why the probes are two
// differently named files.
func TestValidatorGateTriggerCoverage_CodeDepGlobMustSpanWholePackage(t *testing.T) {
	const dep = "go/internal/cigates"
	covered := append(coveredExcept("go/internal/query/**", dep+"/**"), dep+"/a*.go")
	root := writeGateTriggerFixture(t, covered, covered)

	findings, err := validateGateTriggerCoverage(root, gateTriggerContract())
	if err != nil {
		t.Fatalf("validateGateTriggerCoverage() error = %v", err)
	}
	mustContainFinding(t, findings, FindingGateTriggerGap, "selects "+dep)
}

// TestValidatorCodeDepsMatchRealImports derives the anchor set from this
// package's own source so the written-out list cannot drift from the imports it
// claims to describe. Without it, adding an import to the validator would leave
// the new package unanchored and unwatched -- the same "one more input over"
// gap, arriving through a code change instead of a spec edit.
func TestValidatorCodeDepsMatchRealImports(t *testing.T) {
	root := repoRoot(t)
	want := internalPackageClosure(t, root, validatorPackageDir)

	got := append([]string{}, validatorCodeDeps...)
	sort.Strings(got)

	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf(
			"validatorCodeDeps = %v, want %v (derived from %s's own source);\n"+
				"add the package to validatorCodeDeps and give it a trigger in %s and in the %q filter of %s",
			got, want, validatorPackageDir, gateRegistryPath, evidenceFilterKey, gateWorkflowPath,
		)
	}
}

// internalPackageClosure returns the sorted repo-relative directories of every
// first-party package whose source is compiled into the gate's `go test` run of
// dir: dir itself with its test files, plus every first-party package reachable
// from the non-test source of what it imports. A dependency's own test files
// are excluded because this gate never compiles them.
func internalPackageClosure(t *testing.T, root, dir string) []string {
	t.Helper()

	seen := map[string]struct{}{dir: {}}
	queue := []string{dir}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, imported := range firstPartyImports(t, root, current, current == dir) {
			if _, ok := seen[imported]; ok {
				continue
			}
			seen[imported] = struct{}{}
			queue = append(queue, imported)
		}
	}

	dirs := make([]string, 0, len(seen))
	for d := range seen {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	return dirs
}

// firstPartyImports parses the .go files directly in dir and returns the
// repo-relative directories of the first-party packages they import. Build
// constraints are not evaluated, so a build-tagged file contributes its imports
// too; that over-counts rather than under-counts, which is the safe direction
// for a check that decides what the gate must watch.
func firstPartyImports(t *testing.T, root, dir string, includeTests bool) []string {
	t.Helper()

	pkgDir := filepath.Join(root, filepath.FromSlash(dir))
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		t.Fatalf("read package dir %s: %v", dir, err)
	}

	fset := token.NewFileSet()
	var imports []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		if !includeTests && strings.HasSuffix(name, "_test.go") {
			continue
		}
		file := filepath.Join(pkgDir, name)
		parsed, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, spec := range parsed.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("unquote import %s in %s: %v", spec.Path.Value, file, err)
			}
			if !strings.HasPrefix(importPath, firstPartyImportPrefix) {
				continue
			}
			imports = append(imports, strings.TrimPrefix(importPath, firstPartyImportPrefix))
		}
	}
	return imports
}
