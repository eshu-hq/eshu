// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ciJobKey identifies a CI job by its (workflow file, job key) pair, the
// same identity checkCIScriptTriggerCoverage (below) and
// CIScriptTriggerCoverageSummary both key off. A single type shared by both
// keeps "what counts as the same CI job" from drifting between the check
// that skips ambiguous pairs and the summary that reports how many it
// skipped. Split into its own file when adding CIScriptTriggerCoverageSummary
// pushed scripttrigger.go to 501 lines against the repository's 500-line cap;
// the check itself moved here when #9479 added Python imports to the walk.
type ciJobKey struct{ workflow, job string }

// ciJobGateIDs maps every (ci.workflow, ci.job) pair at least one gate
// declares to the IDs of every gate that declares it, in registry order. A
// pair with more than one gate ID is the "shared job" shape
// checkCIScriptTriggerCoverage skips (see its doc comment for why) and
// CIScriptTriggerCoverageSummary counts.
func ciJobGateIDs(reg *Registry) map[ciJobKey][]string {
	m := make(map[ciJobKey][]string, len(reg.Gates))
	for _, g := range reg.Gates {
		if g.CI.Workflow == "" || g.CI.Job == "" {
			continue
		}
		key := ciJobKey{g.CI.Workflow, g.CI.Job}
		m[key] = append(m[key], g.ID)
	}
	return m
}

// CIScriptTriggerCoverageSummary reports how many CI-backed gates
// checkCIScriptTriggerCoverage actually attributes drift to, versus how many
// it structurally skips because their (ci.workflow, ci.job) pair is shared by
// more than one gate and therefore carries no per-gate step attribution.
//
// checkCIScriptTriggerCoverage's own []error slice cannot carry this: a
// clean run returns nil regardless of how many gates were skipped, so
// DriftCheck's caller had no way to print anything but an unqualified "no
// drift" -- true about the gates the check actually walks and silent about
// the ones it does not (#6149 follow-up item 8 review, P2(a)). Run `bash
// scripts/verify-ci-gates-registry.sh --drift` for the live split (the
// `ci-job-trigger-coverage` line this function's caller prints) rather than
// trusting a count here -- this doc comment itself once restated the live
// counts as fixed numbers, in the very file added to stop that pattern, and
// a #6157 review thread caught it. Callers should print this alongside a
// clean drift result so "no drift" reads as "no drift among the N
// attributable gates".
//
// Only counts gates with both ci.workflow and ci.job set; a gate with no CI
// job at all is neither attributed nor skipped by
// checkCIScriptTriggerCoverage, so it is outside this summary too.
func CIScriptTriggerCoverageSummary(reg *Registry) (attributable, skipped int, sharedPairs []string) {
	jobGates := ciJobGateIDs(reg)
	reportedPairs := make(map[ciJobKey]bool, len(jobGates))
	for _, g := range reg.Gates {
		if g.CI.Workflow == "" || g.CI.Job == "" {
			continue
		}
		key := ciJobKey{g.CI.Workflow, g.CI.Job}
		ids := jobGates[key]
		if len(ids) <= 1 {
			attributable++
			continue
		}
		skipped++
		if reportedPairs[key] {
			continue
		}
		reportedPairs[key] = true
		sortedIDs := append([]string(nil), ids...)
		sort.Strings(sortedIDs)
		sharedPairs = append(sharedPairs, fmt.Sprintf(
			"%s/%s (%d gates: %s)", key.workflow, key.job, len(sortedIDs), strings.Join(sortedIDs, ", "),
		))
	}
	sort.Strings(sharedPairs)
	return attributable, skipped, sharedPairs
}

// checkCIScriptTriggerCoverage extends checkScriptTriggerCoverage's
// guarantee to the script CI actually executes, not only the local static
// mirror. The two are routinely different files: ifa-determinism's CI job
// runs the live driver scripts/verify-ifa-determinism.sh while
// local.command runs the static mirror scripts/test-verify-ifa-determinism.sh
// (specs/ci-gates.v1.yaml, ifa-determinism). checkScriptTriggerCoverage walks
// outward only from local.command/local.test_command, so a scripts/ token
// that appears ONLY in the CI job's own run: command -- or that command's own
// sourcing graph, at any depth -- was invisible to it. That gap is not
// hypothetical: it is why three cap-driven split files
// (ifa_deployable_unit_live_diagnostics.sh, ifa_deployable_unit_live_converge.sh,
// and a documentation-family cells split) shipped with no trigger row on a
// prior branch and had to be caught by hand, because the live driver sources
// them and nothing walked that graph.
//
// Narrowed to gates whose ci.workflow and ci.job both resolve, AND whose
// (workflow, job) pair is not shared with another gate. ci.workflow must
// name a readable, parseable file under .github/workflows/, and ci.job must
// be a literal key in that file's `jobs:` map -- a gate whose CI job is a
// matrix-dispatched check (ci.job names an append_gate display or a
// ${{ matrix.* }} job, not a literal jobs: key -- see checkJobNamesResolve)
// is skipped rather than false-flagged, since this check does not attempt
// the matrix-dispatch resolution checkPathFilterCoverage already owns for a
// different purpose.
//
// The shared-pair exclusion is required, not a nicety: a first version of
// this check attributed every `run:` step in the resolved job to whichever
// single gate named it, and measured once against the committed registry at
// the time this narrowing was added -- a historical count, not a live one --
// that produced 352 false positives from exactly one shape -- test.yml's
// verify-contracts job is a single CI job that runs roughly thirty unrelated
// gates' verify scripts as separate named steps, and several other gates in
// the registry declare ci.job: "verify-contracts" as a shared backstop
// rather than a gate-dedicated job (go-core, docs-helm-hygiene, and several
// others follow the same pattern with different member counts). The exact,
// live member count for verify-contracts and every other shared pair is
// CIScriptTriggerCoverageSummary's own output, not a number restated here --
// see that function's doc comment for why a hardcoded count in prose drifts
// from the registry while a derived one cannot. Walking every step
// of a shared job and attributing it to one gate is exactly the wrong
// direction of narrowing: it invents a claim the registry itself does not
// make (that THIS gate owns THAT step), rather than skipping what genuinely
// cannot be attributed -- the same "carries no correspondence signal, so it
// is skipped rather than guessed" convention scriptWorkflowSoundSubset
// already applies to gate-vs-workflow attribution, applied here to
// gate-vs-job-step attribution instead. A (workflow, job) pair used by
// exactly one gate is unambiguous: every run: step in it is that gate's own,
// which is the ifa-determinism/ifa-fault-injection shape this check exists
// for (each has its own dedicated determinism-matrix / fault-injection job).
//
// Within a resolved, unambiguous job, only `run:` steps count -- exactly the
// same "only executable run: blocks count as executing it" convention
// checkVerifyScriptWorkflowMatch already applies for the same reason (a
// dorny/paths-filter entry watches a path, it does not invoke it).
//
// Shares extractScriptPaths, allSourcedScripts, and anyTriggerMatches with
// checkScriptTriggerCoverage, so it inherits every narrowing documented on
// that function's own doc comment (the cd-prefix skip, the literal-"scripts/"-
// text-only sourcing walk, and so on) applied per run: command instead of per
// local.command/local.test_command.
//
// Blind, visibly: this walks reg.Gates only, never hygiene_hooks. A
// hygiene_hooks entry is a bare id+reason pair (specs/ci-gates.v1.yaml's
// `hygiene_hooks:` list) -- it declares no ci.workflow, no ci.job, no
// local.command, nothing this check (or checkScriptTriggerCoverage) could
// walk a script from. That is why this check could not have caught a
// pure-hygiene-hook guard's own self-test going unwired (#6149 follow-up item
// 8 review, P1): the orphaned self-test file was reachable from no gate's
// local.command/local.test_command and no CI job either, and hygiene_hooks is
// where the guard hook already lived at the time, outside every walk in this
// file. Extending this check (or checkScriptTriggerCoverage) into
// hygiene_hooks would need those entries to name a script to walk from in the
// first place, which the schema does not carry today -- a real gap, left open
// rather than silently narrowed further, since closing it changes the
// hygiene_hooks schema, not this function's walk.
//
// The remedy that does not need a schema change: a hygiene hook that grows a
// testable script -- its own local.command, or a self-test -- should become a
// gate, not stay a hook. This is not hypothetical; it is what this same PR
// did four commits earlier, registering the guard's self-test as its own
// local-only gate (a `local.command: ""`, `local.test_command: <the
// self-test>` shape) while the underlying git pre-push hook itself correctly
// stayed a hygiene_hooks entry -- it still has no testable local.command
// shape, the chicken-and-egg problem that gate's own local_only_reason
// explained -- but its self-test did have a testable script and gained a real
// gate instead of staying invisible, which is why this check saw it. (That
// stamp guard and its gate were later removed along with the push stamp.) A
// hygiene_hooks entry with nothing to test (a staged-file variant, a
// commit-msg-stage alias) has no remedy and stays a hook; one whose script
// gains a test does.
//
// Silent, until CIScriptTriggerCoverageSummary: the ONLY visible signal this
// check emits on success is DriftCheck's empty []error, so a clean run reads
// as "no drift" with no scope attached -- some fraction of the registry's
// CI-backed gates sit on a shared (workflow, job) pair and are silently
// skipped by the exclusion above, leaving only the rest this check actually
// attributes drift to. Run `bash scripts/verify-ci-gates-registry.sh
// --drift` for the live split (the `ci-job-trigger-coverage` line) rather
// than trusting a count here -- an earlier version of this exact paragraph
// hardcoded "40 of 93... accounts for 11... leaving only 53" instead of
// deriving it, the same drift-prone pattern the summary function two
// paragraphs above was written to stop, restated by hand in the paragraph
// right next to it. A #6157 review thread caught it. The skip
// logic itself is correct and stays (see the false-positive count above);
// what was missing was a summary saying so. CIScriptTriggerCoverageSummary
// (below) reports the split so a caller can print "no drift among N
// attributable gates" instead of an unqualified "no drift" (#6149 follow-up
// item 8 review, P2(a)).
func checkCIScriptTriggerCoverage(repoRoot string, reg *Registry) []error {
	wfDir := filepath.Join(repoRoot, ".github", "workflows")
	jobGates := ciJobGateIDs(reg)

	var errs []error
	for _, g := range reg.Gates {
		if g.CI.Workflow == "" || g.CI.Job == "" {
			continue
		}
		if len(jobGates[ciJobKey{g.CI.Workflow, g.CI.Job}]) > 1 {
			// A CI job shared by more than one gate carries no per-gate
			// attribution signal -- see the shared-pair discussion above.
			continue
		}
		raw, err := os.ReadFile(filepath.Join(wfDir, g.CI.Workflow)) // #nosec G304 -- wfDir-confined, registry-declared filename
		if err != nil {
			// An unreadable or missing workflow file is a different check's
			// finding (checkWorkflowCompleteness / checkJobNamesResolve);
			// reporting it again here would duplicate, not add, signal.
			continue
		}
		var wf runWorkflowFile
		if yaml.Unmarshal(raw, &wf) != nil {
			continue
		}
		job, ok := wf.Jobs[g.CI.Job]
		if !ok {
			// ci.job did not resolve to a literal jobs: key -- a
			// matrix-dispatched check name, or a genuine mismatch
			// checkJobNamesResolve already reports. Neither is this check's
			// signal to add to.
			continue
		}
		for _, step := range job.Steps {
			if step.Run == "" {
				continue
			}
			for _, script := range extractScriptPaths(step.Run) {
				if !anyTriggerMatches(g.Triggers, script) {
					errs = append(errs, fmt.Errorf(
						"drift: gate %q ci job %q (workflow %s) runs %q but no trigger of that gate matches it (triggers: %v); "+
							"a PR editing only that script would not select the gate locally and would first fail in CI",
						g.ID, g.CI.Job, g.CI.Workflow, script, g.Triggers,
					))
				}
				for _, sourced := range allSourcedScripts(repoRoot, script) {
					if anyTriggerMatches(g.Triggers, sourced) {
						continue
					}
					errs = append(errs, fmt.Errorf(
						"drift: gate %q ci job %q (workflow %s, %s) %s %q but no trigger of that gate matches it (triggers: %v); "+
							"a PR editing only that sourced file would not select the gate locally and would first fail in CI",
						g.ID, g.CI.Job, g.CI.Workflow, script, edgeVerb(sourced), sourced, g.Triggers,
					))
				}
			}
		}
	}
	return errs
}
