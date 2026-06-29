// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import "fmt"

// Selection describes whether a gate was selected for the current run,
// along with the reason it was selected or skipped.
type Selection struct {
	// Gate is the gate entry.
	Gate Gate
	// Selected is true when the gate should be run in this invocation.
	// A gate with Local==nil is never Selected (it is CI-only).
	Selected bool
	// Reason is a human-readable explanation of the selection decision.
	Reason string
}

// Select evaluates each gate in registry order against the provided changed
// paths and the requested tier ceiling. It returns one Selection per gate,
// preserving registry order. The result is a pure function of its inputs.
//
// Selection rules:
//  1. ci-heavy and manual tiers are never selected by a local tier request;
//     they appear as skipped with a tier reason.
//  2. A gate whose tier exceeds the requested tier ceiling is skipped.
//  3. A gate with Local==nil is CI-only; it is never Selected but is reported
//     with its CIOnlyReason.
//  4. A gate is selected when at least one of its Triggers matches at least
//     one of the changed paths.
func (r *Registry) Select(changed []string, tier Tier) []Selection {
	sels := make([]Selection, 0, len(r.Gates))
	for _, g := range r.Gates {
		sels = append(sels, selectGate(g, changed, tier))
	}
	return sels
}

func selectGate(g Gate, changed []string, requestedTier Tier) Selection {
	// ci-heavy and manual are never run locally regardless of the requested tier.
	if g.Tier == TierCIHeavy || g.Tier == TierManual {
		return Selection{
			Gate:     g,
			Selected: false,
			Reason:   fmt.Sprintf("tier %s is CI/manual-only — skipped in local lane", g.Tier),
		}
	}

	// The gate's tier must be at most the requested tier.
	if !TierAtMost(g.Tier, requestedTier) {
		return Selection{
			Gate:     g,
			Selected: false,
			Reason:   fmt.Sprintf("tier %s exceeds requested ceiling %s", g.Tier, requestedTier),
		}
	}

	// CI-only gates are never selected but are always reported.
	if g.Local == nil {
		return Selection{
			Gate:     g,
			Selected: false,
			Reason:   fmt.Sprintf("CI-only: %s", g.CIOnlyReason),
		}
	}

	// Check triggers against changed paths.
	for _, trigger := range g.Triggers {
		for _, path := range changed {
			if MatchGlob(trigger, path) {
				return Selection{
					Gate:     g,
					Selected: true,
					Reason:   fmt.Sprintf("matched trigger %q on path %q", trigger, path),
				}
			}
		}
	}

	return Selection{
		Gate:     g,
		Selected: false,
		Reason:   "no trigger matched changed paths",
	}
}
