// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"sort"
	"strings"
)

// ciJobKey identifies a CI job by its (workflow file, job key) pair, the
// same identity checkCIScriptTriggerCoverage (scripttrigger.go) and
// CIScriptTriggerCoverageSummary both key off. A single type shared by both
// keeps "what counts as the same CI job" from drifting between the check
// that skips ambiguous pairs and the summary that reports how many it
// skipped. Split into its own file when adding CIScriptTriggerCoverageSummary
// pushed scripttrigger.go to 501 lines against the repository's 500-line cap.
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
// drift" -- which is a true statement about the 53 gates the check actually
// walks and a silent one about the 40 it does not (test.yml/verify-contracts
// alone accounts for 11 of them). Callers should print this alongside a
// clean drift result so "no drift" reads as "no drift among the N
// attributable gates" (#6149 follow-up item 8 review, P2(a)).
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
