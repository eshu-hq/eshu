// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import (
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/goldengate"
)

// Status is the coverage outcome for one supported surface.
type Status string

const (
	// StatusCovered means the surface has a manifest entry whose scenario
	// artifact resolved.
	StatusCovered Status = "covered"
	// StatusUncovered means the surface has no manifest entry and is not exempt:
	// it is part of the C-2..C-6 worklist.
	StatusUncovered Status = "uncovered"
	// StatusUnresolved means the surface has a manifest entry but its referenced
	// scenario artifact is missing — broken coverage that must be fixed.
	StatusUnresolved Status = "unresolved"
	// StatusExempt means the surface is deliberately not required to have a
	// scenario, with a recorded reason.
	StatusExempt Status = "exempt"
)

// SurfaceCoverage is the reconciled coverage outcome for one supported surface.
type SurfaceCoverage struct {
	// Surface is the supported surface being reconciled.
	Surface SupportedSurface
	// Status is the coverage outcome.
	Status Status
	// Scenario is the resolved manifest entry, when the surface is covered or
	// unresolved; nil for uncovered/exempt.
	Scenario *CoverageEntry
	// Exemption is the recorded exemption when the surface is exempt; nil otherwise.
	Exemption *Exemption
	// Detail is a short human explanation (resolver detail or exemption reason).
	Detail string
}

// Coverage is the full reconciliation of supported surfaces against the manifest.
type Coverage struct {
	// Surfaces are the per-surface coverage outcomes, sorted by registry then key.
	Surfaces []SurfaceCoverage
	// Stale are manifest coverage/exemption surfaces that match no supported
	// surface — manifest drift (a surface was renamed or removed). Sorted.
	Stale []string
}

// Reconcile maps every supported surface to its coverage outcome using the
// manifest and resolver, and collects manifest entries that match no supported
// surface as stale drift. The output is deterministic: surfaces keep the sorted
// order EnumerateSupported produced and Stale is sorted.
func Reconcile(supported []SupportedSurface, m Manifest, r Resolver) Coverage {
	coverageBySurface := map[string]CoverageEntry{}
	for _, e := range m.Coverage {
		coverageBySurface[e.Surface] = e
	}
	exemptBySurface := map[string]Exemption{}
	for _, e := range m.Exemptions {
		exemptBySurface[e.Surface] = e
	}

	supportedKeys := map[string]struct{}{}
	var out Coverage
	for _, s := range supported {
		supportedKeys[s.Key] = struct{}{}
		sc := SurfaceCoverage{Surface: s}
		switch ex, exempt := exemptBySurface[s.Key]; {
		case exempt:
			exCopy := ex
			sc.Status = StatusExempt
			sc.Exemption = &exCopy
			sc.Detail = ex.Reason
		default:
			entry, ok := coverageBySurface[s.Key]
			if !ok {
				sc.Status = StatusUncovered
				sc.Detail = "no replay scenario mapped"
				break
			}
			resolved, detail := r.Resolve(entry)
			entryCopy := entry
			sc.Scenario = &entryCopy
			sc.Detail = detail
			if resolved {
				sc.Status = StatusCovered
			} else {
				sc.Status = StatusUnresolved
			}
		}
		out.Surfaces = append(out.Surfaces, sc)
	}

	for _, e := range m.Coverage {
		if _, ok := supportedKeys[e.Surface]; !ok {
			out.Stale = append(out.Stale, e.Surface)
		}
	}
	for _, e := range m.Exemptions {
		if _, ok := supportedKeys[e.Surface]; !ok {
			out.Stale = append(out.Stale, e.Surface)
		}
	}
	sort.Strings(out.Stale)
	return out
}

// Findings renders the reconciliation as goldengate findings so the coverage gate
// reuses the shared advisory→blocking report machinery. When blocking is false
// (the shipped default) every finding is advisory and the gate never fails on a
// coverage gap; the single blocking flag flips uncovered, unresolved, and stale
// findings to required so coverage can never regress once C-2..C-6 burn the gaps
// down. Covered and exempt surfaces are always OK.
func Findings(c Coverage, blocking bool) []goldengate.Finding {
	var findings []goldengate.Finding
	for _, sc := range c.Surfaces {
		ok := sc.Status == StatusCovered || sc.Status == StatusExempt
		findings = append(findings, goldengate.Finding{
			Phase:    string(sc.Surface.Registry),
			Check:    sc.Surface.Key,
			OK:       ok,
			Required: blocking,
			Detail:   fmt.Sprintf("%s: %s", sc.Status, sc.Detail),
		})
	}
	for _, surface := range c.Stale {
		findings = append(findings, goldengate.Finding{
			Phase:    "manifest",
			Check:    surface,
			OK:       false,
			Required: blocking,
			Detail:   "stale: manifest entry maps no supported surface",
		})
	}
	return findings
}
