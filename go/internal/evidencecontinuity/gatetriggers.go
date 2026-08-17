// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidencecontinuity

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// evidenceContinuityGateID is this validator's own entry in
// specs/ci-gates.v1.yaml. The trigger-coverage check reads that entry back,
// so a rename of the gate id fails loudly here instead of silently detaching
// the check from the gate.
const evidenceContinuityGateID = "evidence-continuity"

// evidenceFilterKey is the dorny/paths-filter key that selects the
// evidence-continuity CI job in static-contract-gates.yml.
const evidenceFilterKey = "evidence"

// contractSpecPath is the repo-relative path of the evidence-continuity
// matrix. It must stay a trigger on both sides, because an edit to it can
// create a trigger blind spot, so having it in the trigger set guarantees the
// check runs on the edit that would create the blindness.
const contractSpecPath = "specs/evidence-continuity.v1.yaml"

// validatorInputAnchors are every file family ValidateRepository reads whose
// edit could invalidate a result this gate reports. There are three, all
// reached from the same call: the contract spec, the capability matrix and its
// fragments (LoadSurfaceIndex -> loadCapabilities), and the generated surface
// inventory (LoadSurfaceIndex -> loadSurfaces). Anchoring only the contract
// left a capability-id rename able to pass this gate green and surface later as
// `unknown_capability` on an unrelated pull request -- the exact blind-spot
// class the gate exists to close, re-opened one input over.
//
// The surface inventory is anchored explicitly even though the repo covers it
// today through the "go/internal/capabilitycatalog/**" trigger the package
// check already demands. That coverage is incidental, not enforced: the package
// check probes only _test.go files directly in the package root
// (packageTestProbes), so narrowing that trigger to "*_test.go" keeps the
// package check green while dropping data/ -- and a regeneration removing a
// route or tool would then surface as `unknown_api_route`/`unknown_mcp_tool` on
// an unrelated pull request. Anchoring the file makes the coverage a stated
// requirement rather than a side effect of how another check happens to be
// written.
//
// The fragment entry is a representative path rather than a glob: the check
// asks whether the trigger globs would select a file in that directory, so any
// name under it answers the question.
var validatorInputAnchors = []string{
	contractSpecPath,
	"specs/capability-matrix.v1.yaml",
	"specs/capability-matrix/a.yaml",
	"go/internal/capabilitycatalog/data/surface-inventory.generated.json",
}

// validateGateTriggerCoverage asserts that the evidence-continuity gate can
// see every file whose edit could invalidate the contract's `go test` proof
// refs. For each package a proof ref names, both the gate's registry triggers
// (specs/ci-gates.v1.yaml) and its CI workflow path filter
// (static-contract-gates.yml, dorny filter key "evidence") must span the
// package's _test.go files — the exact file set loadPackageTestNames reads.
// Before #6131 the trigger set was disjoint from the referenced packages, so
// renaming a referenced test selected nothing locally and broke CI on
// unrelated PRs; this check makes that gap a gate failure on the spec edit
// that would create it.
//
// The validator-input anchors below are checked on every call, including for a
// contract with no go-test refs. They do not depend on the package half having
// work to do: ValidateRepository reads the capability matrix and the surface
// inventory whatever the proof refs look like, so gating the anchors on a
// non-empty package set would let stripping the go-test refs from the spec
// silently retire the anchor check too.
func validateGateTriggerCoverage(repoRoot string, contract Contract) ([]Finding, error) {
	packageDirs := proofRefPackageDirs(contract)

	triggers, err := evidenceGateTriggers(repoRoot)
	if err != nil {
		return nil, err
	}
	filterPaths, err := evidenceWorkflowFilter(repoRoot)
	if err != nil {
		return nil, err
	}

	sides := []struct {
		label string
		globs []string
	}{
		{"gate trigger in specs/ci-gates.v1.yaml", triggers},
		{fmt.Sprintf("path filter %q entry in .github/workflows/static-contract-gates.yml", evidenceFilterKey), filterPaths},
	}
	var findings []Finding
	for _, side := range sides {
		label := side.label
		for _, dir := range packageDirs {
			if oneGlobMatchesAll(side.globs, packageTestProbes(dir)) {
				continue
			}
			// RowID stays empty: these findings describe the gate's trigger
			// wiring, not a matrix row, and every other Finding uses RowID as
			// a contract row id (the message names the gate).
			findings = append(findings, Finding{
				Kind: FindingGateTriggerGap,
				Message: fmt.Sprintf(
					"proof refs run tests in %s but no %s spans that package's _test.go files; "+
						"a referenced-test rename there would not select the evidence-continuity gate — add %q",
					dir, label, dir+"/**",
				),
			})
		}
		for _, anchor := range validatorInputAnchors {
			if oneGlobMatchesAll(side.globs, []string{anchor}) {
				continue
			}
			findings = append(findings, Finding{
				Kind: FindingGateTriggerGap,
				Message: fmt.Sprintf(
					"no %s selects %s, which ValidateRepository reads; an edit there could create a trigger blind spot and must always run this gate",
					label, anchor,
				),
			})
		}
	}
	return findings, nil
}

// proofRefPackageDirs returns the sorted, deduplicated repo-relative
// directories (slash-separated, e.g. "go/internal/query") of every Go package
// named by a parseable `go test` proof ref in the contract.
func proofRefPackageDirs(contract Contract) []string {
	seen := map[string]struct{}{}
	for _, site := range collectProofRefs(contract) {
		packages, _, ok := parseGoTestRef(site.ref)
		if !ok {
			continue
		}
		for _, pkg := range packages {
			pkg = strings.TrimSpace(pkg)
			// Mirror packageDir's acceptance rule; refs it rejects are
			// already reported through validateReferencedTests.
			if !strings.HasPrefix(pkg, "./") || strings.Contains(pkg, "..") {
				continue
			}
			seen[path.Join("go", strings.TrimPrefix(pkg, "./"))] = struct{}{}
		}
	}
	dirs := make([]string, 0, len(seen))
	for dir := range seen {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	return dirs
}

// packageTestProbes returns the synthetic paths a single glob must match to
// count as covering a package's test files. Two differently named probes are
// required so a filename-narrowed glob (e.g. "dir/a*_test.go") cannot read as
// package coverage while excluding other test files — the same directory-wide
// demand cigates' goPackageDir probes make, restricted to _test.go because
// files directly in the package dir ending in _test.go are exactly what
// loadPackageTestNames parses.
func packageTestProbes(dir string) []string {
	return []string{
		path.Join(dir, "a_test.go"),
		path.Join(dir, "z_test.go"),
	}
}

// oneGlobMatchesAll reports whether a single glob in globs matches every
// probe. One glob must span them all: two globs that each match a different
// probe do not prove any single pattern covers the set.
func oneGlobMatchesAll(globs, probes []string) bool {
	for _, glob := range globs {
		all := true
		for _, probe := range probes {
			if !cigates.MatchGlob(glob, probe) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// evidenceGateTriggers loads the registry and returns the evidence-continuity
// gate's trigger globs, failing loudly when the registry or the gate entry is
// missing rather than letting the coverage check pass vacuously.
func evidenceGateTriggers(repoRoot string) ([]string, error) {
	registryPath := filepath.Join(repoRoot, "specs", "ci-gates.v1.yaml")
	registry, err := cigates.Load(registryPath)
	if err != nil {
		return nil, fmt.Errorf("load ci-gates registry for trigger coverage: %w", err)
	}
	for _, gate := range registry.Gates {
		if gate.ID == evidenceContinuityGateID {
			return gate.Triggers, nil
		}
	}
	return nil, fmt.Errorf("ci-gates registry %s has no %q gate; trigger coverage cannot be checked", registryPath, evidenceContinuityGateID)
}

// evidenceWorkflowFilter returns the "evidence" dorny path-filter globs from
// the static-contract-gates workflow, failing loudly when the workflow, the
// dorny step, or the filter key is missing — or when the step sets
// `predicate-quantifier: every`. oneGlobMatchesAll proves coverage under
// dorny's default "some" quantifier (ANY pattern selects the file); under
// `every` a file is selected only when ALL patterns match, so the same proof
// would report coverage it did not establish. Three sibling workflows already
// set `every`, so refusing here keeps a copy-paste of that step from turning
// this self-check false-green.
func evidenceWorkflowFilter(repoRoot string) ([]string, error) {
	workflowPath := filepath.Join(repoRoot, ".github", "workflows", "static-contract-gates.yml")
	raw, err := os.ReadFile(workflowPath) // #nosec G304 -- static verifier reads a repo-local workflow path, not request input.
	if err != nil {
		return nil, fmt.Errorf("read workflow for trigger coverage: %w", err)
	}
	filters, every := cigates.DornyFilters(raw)
	if filters == nil {
		return nil, fmt.Errorf("workflow %s has no parsable dorny/paths-filter step; trigger coverage cannot be checked", workflowPath)
	}
	if every {
		return nil, fmt.Errorf("workflow %s dorny step sets predicate-quantifier: every; this coverage check only proves coverage under the default ANY-pattern semantics — route the evidence filter through a step without `every`, or extend the check to model it", workflowPath)
	}
	paths, ok := filters[evidenceFilterKey]
	if !ok || len(paths) == 0 {
		return nil, fmt.Errorf("workflow %s dorny filters have no %q entry; trigger coverage cannot be checked", workflowPath, evidenceFilterKey)
	}
	return paths, nil
}
