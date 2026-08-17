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

// The repo-relative paths ValidateRepository reads. The contract spec, the gate
// registry, and the workflow are joined onto repoRoot at their read sites, so
// those three cannot drift from the anchor list below. The capability matrix,
// its fragment directory, and the surface inventory are still mirrors of the
// paths LoadSurfaceIndex builds from its specs/ and data/ arguments, so
// TestValidatorInputPathsExistInRepo resolves every one against this repository:
// a rename would otherwise leave an anchor watching a path that no longer
// exists, which passes the coverage check while the real input goes unwatched.
const (
	// contractSpecPath is the evidence-continuity matrix.
	contractSpecPath = "specs/evidence-continuity.v1.yaml"
	// capabilityMatrixPath is the capability matrix root file.
	capabilityMatrixPath = "specs/capability-matrix.v1.yaml"
	// capabilityFragmentDir holds the matrix fragments, read non-recursively
	// and .yaml-only by loadCapabilities.
	capabilityFragmentDir = "specs/capability-matrix"
	// surfaceInventoryPath is the generated API-route/MCP-tool inventory.
	surfaceInventoryPath = "go/internal/capabilitycatalog/data/surface-inventory.generated.json"
	// gateRegistryPath is the CI gate registry this check reads its own gate's
	// triggers back from.
	gateRegistryPath = "specs/ci-gates.v1.yaml"
	// gateWorkflowPath is the workflow whose "evidence" dorny filter selects
	// this gate in CI.
	gateWorkflowPath = ".github/workflows/static-contract-gates.yml"
)

// validatorInput is one file family ValidateRepository reads whose edit could
// invalidate a result this gate reports, paired with the probe paths a single
// trigger glob must match to count as covering it.
//
// A family backed by a directory carries two differently named probes for the
// same reason packageTestProbes does: one probe cannot tell a directory-wide
// glob from a filename-narrowed one that leaves the siblings uncovered, so a
// lone "specs/capability-matrix/a.yaml" probe would accept
// "specs/capability-matrix/a*.yaml" and drop every fragment not starting with
// an "a".
type validatorInput struct {
	// display names the family in a finding message.
	display string
	// probes are the paths one trigger glob must match, all of them, to count
	// as covering the family.
	probes []string
}

// validatorInputs are every file family ValidateRepository reads, all reached
// from the same call: the contract spec, the capability matrix and its
// fragments (LoadSurfaceIndex -> loadCapabilities), the generated surface
// inventory (LoadSurfaceIndex -> loadSurfaces), and the two files
// validateGateTriggerCoverage itself opens to decide the coverage below -- the
// CI gate registry and the workflow.
//
// Anchoring only the contract left a capability-id rename able to pass this
// gate green and surface later as `unknown_capability` on an unrelated pull
// request -- the exact blind-spot class the gate exists to close, re-opened one
// input over. The registry and workflow entries close it again on the two
// inputs this check itself introduced: nothing else in the repo requires a gate
// to trigger on either file, so dropping one from BOTH trigger sets passes
// every gate today, and the next PR narrowing a package trigger would then
// never run this check at all.
//
// The surface inventory is anchored explicitly even though the repo covers it
// today through the "go/internal/capabilitycatalog/**" trigger the package
// check already demands. That coverage is incidental, not enforced: the package
// check probes only _test.go files directly in the package root
// (packageTestProbes), so narrowing that trigger to "*_test.go" keeps the
// package check green while dropping data/ -- and a regeneration removing a
// route or tool would then surface as `unknown_api_route`/`unknown_mcp_tool` on
// an unrelated pull request. Anchoring a file makes the coverage a stated
// requirement rather than a side effect of how another check happens to be
// written, which is why every entry is listed whether or not something else
// covers it today.
var validatorInputs = []validatorInput{
	{display: contractSpecPath, probes: []string{contractSpecPath}},
	{display: capabilityMatrixPath, probes: []string{capabilityMatrixPath}},
	{
		display: capabilityFragmentDir + "/*.yaml",
		probes: []string{
			path.Join(capabilityFragmentDir, "a.yaml"),
			path.Join(capabilityFragmentDir, "z.yaml"),
		},
	},
	{display: surfaceInventoryPath, probes: []string{surfaceInventoryPath}},
	{display: gateRegistryPath, probes: []string{gateRegistryPath}},
	{display: gateWorkflowPath, probes: []string{gateWorkflowPath}},
}

// validatorPackageDir is this validator's own package, relative to the repo
// root. Its source decides every finding the gate reports, and the gate's local
// command runs `go test` here.
const validatorPackageDir = "go/internal/evidencecontinuity"

// validatorCodeDeps are the repo-relative directories of every Go package whose
// source decides what this gate reports: this validator's own package plus the
// first-party packages it imports, transitively. This is the third category the
// trigger sets must span, after the proof refs' packages and validatorInputs --
// a code dependency rather than a data file.
//
// The two categories above watch what the validator reads; neither watches what
// it is built from. validateGateTriggerCoverage calls cigates.Load,
// cigates.MatchGlob, and cigates.DornyFilters, so a semantic change to
// MatchGlob or DornyFilters changes what gate_trigger_gap reports -- yet before
// #6131's follow-up neither trigger set named go/internal/cigates, so a
// cigates-only edit never selected this gate. The validator's own package is
// listed for the same reason one layer in: no `go test` proof ref names it, so
// the package half of this check never demands it either, and dropping it from
// both trigger sets would pass every gate today.
//
// TestValidatorCodeDepsMatchRealImports derives this set from the package's own
// source and fails when the two disagree, so a new import cannot land without
// either listing it here or deleting that test. The list is written out rather
// than derived at run time because the fixtures below are bare temp roots
// holding only a registry and a workflow: a validator that read its own source
// out of repoRoot would derive an empty set there, and the anchor would
// silently check nothing in every test that exercises it.
var validatorCodeDeps = []string{
	validatorPackageDir,
	"go/internal/cigates",
}

// validateGateTriggerCoverage asserts that the evidence-continuity gate can
// see every file whose edit could change what it reports. That is three file
// sets: the packages the contract's `go test` proof refs name, the inputs
// ValidateRepository reads (validatorInputs), and the packages the validator is
// built from (validatorCodeDeps). Both the gate's registry
// triggers (specs/ci-gates.v1.yaml) and its CI workflow path filter
// (static-contract-gates.yml, dorny filter key "evidence") must span each.
// For a package that means its _test.go files — the exact file set
// loadPackageTestNames reads.
// Before #6131 the trigger set was disjoint from the referenced packages, so
// renaming a referenced test selected nothing locally and broke CI on
// unrelated PRs; this check makes that gap a gate failure on the edit that
// would create it, which is why every input it reads is itself anchored below.
//
// The validator inputs below are checked on every call, including for a
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
		{fmt.Sprintf("gate trigger in %s", gateRegistryPath), triggers},
		{fmt.Sprintf("path filter %q entry in %s", evidenceFilterKey, gateWorkflowPath), filterPaths},
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
		for _, input := range validatorInputs {
			if oneGlobMatchesAll(side.globs, input.probes) {
				continue
			}
			findings = append(findings, Finding{
				Kind: FindingGateTriggerGap,
				Message: fmt.Sprintf(
					"no %s selects %s, which ValidateRepository reads; an edit there could create a trigger blind spot and must always run this gate",
					label, input.display,
				),
			})
		}
		for _, dep := range validatorCodeDeps {
			if oneGlobMatchesAll(side.globs, packageSourceProbes(dep)) {
				continue
			}
			findings = append(findings, Finding{
				Kind: FindingGateTriggerGap,
				Message: fmt.Sprintf(
					"no %s selects %s, whose Go source this validator is built from; a change there could alter what this gate reports without ever running it — add %q",
					label, dep, dep+"/**",
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

// packageSourceProbes returns the synthetic paths a single glob must match to
// count as covering a Go package's compiled source: files ending in .go
// directly in the package directory, which is exactly what the compiler builds
// for that package. Two differently named probes reject a filename-narrowed
// glob for the same reason packageTestProbes does — "go/internal/cigates/g*.go"
// matches the file MatchGlob lives in today and would read as coverage while
// leaving every sibling outside the gate's reach. A `dir/*.go` glob spans the
// package's test files too, so no separate _test.go probe is needed here.
func packageSourceProbes(dir string) []string {
	return []string{
		path.Join(dir, "a.go"),
		path.Join(dir, "z.go"),
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
	registryPath := filepath.Join(repoRoot, filepath.FromSlash(gateRegistryPath))
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
	workflowPath := filepath.Join(repoRoot, filepath.FromSlash(gateWorkflowPath))
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
