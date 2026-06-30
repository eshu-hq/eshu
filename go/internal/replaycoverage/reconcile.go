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
	// it is part of the C-lane coverage worklist.
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
	// ScenarioType is the required depth scenario type being reconciled for the
	// surface. Exempt surfaces use the baseline type so they still occupy one
	// auditable row in reports.
	ScenarioType DepthScenarioType
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
	coverageByRequirement := map[string]CoverageEntry{}
	for _, e := range m.Coverage {
		coverageByRequirement[manifestCoverageKey(e.Surface, e.ScenarioType)] = e
	}
	exemptBySurface := map[string]Exemption{}
	for _, e := range m.Exemptions {
		exemptBySurface[e.Surface] = e
	}
	requirementsBySurface := map[string][]DepthScenarioType{}
	for _, req := range m.Requirements {
		requirementsBySurface[req.Surface] = append([]DepthScenarioType(nil), req.ScenarioTypes...)
	}

	supportedKeys := map[string]struct{}{}
	supportedRequirementKeys := map[string]struct{}{}
	var out Coverage
	for _, s := range supported {
		supportedKeys[s.Key] = struct{}{}
		switch ex, exempt := exemptBySurface[s.Key]; {
		case exempt:
			exCopy := ex
			// A baseline exemption must not silently suppress a surface's derived
			// depth requirements (e.g. fault for a collector boundary). Emit an
			// exempt row for every required scenario_type so a no-upstream collector
			// is visibly fault-exempt, not absent from the C-14 worklist. Surfaces
			// with no derived depth requirement keep their single baseline row.
			for _, scenarioType := range requiredScenarioTypes(s.Key, requirementsBySurface) {
				supportedRequirementKeys[manifestCoverageKey(s.Key, scenarioType)] = struct{}{}
				out.Surfaces = append(out.Surfaces, SurfaceCoverage{
					Surface:      s,
					ScenarioType: scenarioType,
					Status:       StatusExempt,
					Exemption:    &exCopy,
					Detail:       ex.Reason,
				})
			}
		default:
			for _, scenarioType := range requiredScenarioTypes(s.Key, requirementsBySurface) {
				supportedRequirementKeys[manifestCoverageKey(s.Key, scenarioType)] = struct{}{}
				sc := SurfaceCoverage{Surface: s, ScenarioType: scenarioType}
				entry, ok := coverageByRequirement[manifestCoverageKey(s.Key, scenarioType)]
				if !ok {
					sc.Status = StatusUncovered
					sc.Detail = fmt.Sprintf("no replay scenario mapped for required scenario_type %s", scenarioType)
					out.Surfaces = append(out.Surfaces, sc)
					continue
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
				out.Surfaces = append(out.Surfaces, sc)
			}
		}
	}

	for _, e := range m.Coverage {
		if _, ok := supportedKeys[e.Surface]; !ok {
			out.Stale = append(out.Stale, coverageDisplayKey(e.Surface, e.ScenarioType))
			continue
		}
		if _, ok := supportedRequirementKeys[manifestCoverageKey(e.Surface, e.ScenarioType)]; !ok {
			out.Stale = append(out.Stale, coverageDisplayKey(e.Surface, e.ScenarioType))
		}
	}
	for _, req := range m.Requirements {
		if _, ok := supportedKeys[req.Surface]; !ok {
			out.Stale = append(out.Stale, req.Surface)
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

func requiredScenarioTypes(surface string, requirementsBySurface map[string][]DepthScenarioType) []DepthScenarioType {
	if reqs, ok := requirementsBySurface[surface]; ok {
		return append([]DepthScenarioType(nil), reqs...)
	}
	return []DepthScenarioType{ScenarioTypeBaseline}
}

// Findings renders the reconciliation as goldengate findings so the coverage gate
// reuses the shared advisory→blocking report machinery. When blocking is false,
// every finding is advisory and the gate never fails on a coverage gap; CI passes
// the single blocking flag so uncovered, unresolved, and stale findings are
// required now that the C-lane gaps have burned down. Covered and exempt
// surfaces are always OK.
func Findings(c Coverage, blocking bool) []goldengate.Finding {
	var findings []goldengate.Finding
	for _, sc := range c.Surfaces {
		ok := sc.Status == StatusCovered || sc.Status == StatusExempt
		findings = append(findings, goldengate.Finding{
			Phase:    string(sc.Surface.Registry),
			Check:    coverageDisplayKey(sc.Surface.Key, sc.ScenarioType),
			OK:       ok,
			Required: blocking && isBlockingScenarioType(sc.ScenarioType),
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

// isBlockingScenarioType reports whether a depth class fails the blocking gate.
// Only baseline (the C-1 breadth contract) is blocking; the C-8/C-13 depth classes
// are advisory-first (#4366): the gate enumerates and reports the missing
// surface x depth pairs (the C-14 worklist) without failing CI, until C-14 burns
// them down and a later ticket flips depth to blocking.
func isBlockingScenarioType(t DepthScenarioType) bool {
	return t == "" || t == ScenarioTypeBaseline
}

func coverageDisplayKey(surface string, scenarioType DepthScenarioType) string {
	if scenarioType == "" || scenarioType == ScenarioTypeBaseline {
		return surface
	}
	return manifestCoverageKey(surface, scenarioType)
}
