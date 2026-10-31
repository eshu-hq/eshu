// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"strings"
)

// parseGeneratorInputs trims and validates a gate's generator_inputs list,
// the declared machine-readable source of the file classes whose changes can
// alter that gate's regenerated output (#9621). Like triggers, the list is a
// set: blank and duplicate entries are rejected at load.
func parseGeneratorInputs(path, gateID string, inputs []string) ([]string, error) {
	out := make([]string, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	for _, in := range inputs {
		trimmed := strings.TrimSpace(in)
		if trimmed == "" {
			return nil, fmt.Errorf("ci-gates registry %s: gate %q has a blank generator_inputs entry (generator_inputs: must name file classes, not blanks)", path, gateID)
		}
		if _, dup := seen[trimmed]; dup {
			return nil, fmt.Errorf("ci-gates registry %s: gate %q has duplicate generator_inputs entry %q (generator_inputs: must be a set, not a list)", path, gateID, trimmed)
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out, nil
}

// sampleForInputClass derives one representative file path from a generator
// input class glob. A "**" segment becomes one "sample" segment and every "*"
// inside a segment becomes "sample" text, so "go/**/*.go" yields
// "go/sample/sample.go" and a literal input yields itself. The sample is only
// ever matched against trigger globs with MatchGlob — the same primitive
// Select uses for non-docs paths — never stat-checked, so an input class that
// names files that do not exist yet still proves its selection.
func sampleForInputClass(input string) string {
	segments := strings.Split(input, "/")
	for i, segment := range segments {
		if segment == "**" {
			segments[i] = "sample"
			continue
		}
		segments[i] = strings.ReplaceAll(segment, "*", "sample")
	}
	return strings.Join(segments, "/")
}

// checkGeneratorInputCoverageForDrift is the DriftCheck entry point for the
// #9621 subcheck. Fixture registries with no regenerate map and no declared
// inputs skip the tracked-universe enumeration entirely, so the existing
// hermetic drift tests pay nothing; a universe that cannot be enumerated
// yields a nil tracked set, which checkGeneratorInputCoverage treats like
// checkGlobTriggerResolves does (Validate already reported that failure).
func checkGeneratorInputCoverageForDrift(repoRoot string, reg *Registry) []error {
	needed := false
	for _, g := range reg.Gates {
		if len(g.Regenerate) > 0 || len(g.GeneratorInputs) > 0 {
			needed = true
			break
		}
	}
	if !needed {
		return nil
	}
	tracked, err := loadTrackedPaths(repoRoot)
	if err != nil {
		return checkGeneratorInputCoverage(repoRoot, reg, nil)
	}
	return checkGeneratorInputCoverage(repoRoot, reg, tracked)
}

// checkGeneratorInputCoverage enforces the #9621 contract: a gate whose
// command regenerates committed output and diffs it — machine-identified by a
// non-empty regenerate: map, the same map regen-conflicts.sh reads — must
// declare the generator's input classes in generator_inputs, and every
// declared class must be matched by at least one of the gate's own triggers.
// Otherwise a PR changing only a generator input selects no gate locally and
// first fails in CI, the #9588 shape that turned main red on the merge push
// (#9614). Gates without a regenerate map may still declare generator_inputs
// to gain the same proof; gates with neither are untouched by this check.
//
// A declared class must also resolve to at least one tracked file, mirroring
// checkGlobTriggerResolves: an input class matching nothing proves selection
// of nothing. tracked is nil only when the universe could not be enumerated,
// which the caller already reported as its own error; skipping there reports
// that failure once instead of once per input, and the run still fails.
//
// Coverage across every regenerate-and-diff gate, verified against the
// committed registry with the real selector:
//
//   - live-readiness-registration-drift, env-registry-doc,
//     collector-content-pin, ci-gate-registry, sdk-go-factschema,
//     collector-entrypoints-generated, remote-validation-artifacts,
//     capability-surface-inventory: declare generator_inputs; the drift check
//     below proves their triggers cover every class. The surface gate has no
//     regenerate map (its local command verifies in place) and declares its
//     inputs voluntarily to gain the same proof (#9655).
//   - slim-collector-entrypoints: intentionally uncovered. It is the pre-push
//     slim-lane mirror of collector-entrypoints-generated; its input classes
//     are owned by that pre-pr gate. Widening slim triggers changes
//     merge-queue selection and needs its own decision.
//   - operator-dashboard, go-coverage: intentionally uncovered. Their local
//     commands regenerate unconditionally and never diff, so a selection gap
//     rewrites rather than reddens — there is no stale-output failure to
//     select for.

func checkGeneratorInputCoverage(repoRoot string, reg *Registry, tracked *trackedPaths) []error {
	var errs []error
	for _, g := range reg.Gates {
		if len(g.Regenerate) > 0 && len(g.GeneratorInputs) == 0 {
			errs = append(errs, fmt.Errorf(
				"drift: gate %q owns regenerated output but declares no generator_inputs — name every file class the generator reads, or a PR changing only that input skips this gate locally and first fails in CI",
				g.ID,
			))
			continue
		}
		for _, input := range g.GeneratorInputs {
			if strings.TrimSpace(input) == "" {
				errs = append(errs, fmt.Errorf(
					"drift: gate %q has a blank generator_inputs entry — generator_inputs: must name file classes, not blanks",
					g.ID,
				))
				continue
			}
			sample := sampleForInputClass(input)
			if !anyTriggerMatches(g.Triggers, sample) {
				errs = append(errs, fmt.Errorf(
					"drift: gate %q generator input %q (sample %q) is matched by no trigger of that gate (triggers: %v); "+
						"a PR changing only that input would not select the gate locally and would first fail in CI",
					g.ID, input, sample, g.Triggers,
				))
			}
			if tracked != nil && !tracked.matchesAny(input) {
				errs = append(errs, fmt.Errorf(
					"drift: gate %q generator input %q matches no tracked file under %q — an input class that resolves to nothing can never select this gate, so the surface it names silently loses its guard",
					g.ID, input, repoRoot,
				))
			}
		}
	}
	return errs
}
