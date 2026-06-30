// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import (
	"encoding/json"
	"fmt"
	"sort"
)

// ReportSchemaVersion is the coverage-report artifact schema version. C-7
// (coverage dashboard) consumes this artifact, so the version is bumped when the
// report shape changes. v3 adds the C-11 (#4364) language-parser scoreboard.
const ReportSchemaVersion = "replay-coverage-report.v3"

// RegistrySummary is the per-registry (or grand-total) coverage tally. The grand
// total uses an empty Registry.
type RegistrySummary struct {
	Registry         Registry `json:"registry,omitempty"`
	Total            int      `json:"total"`
	Covered          int      `json:"covered"`
	Uncovered        int      `json:"uncovered"`
	Unresolved       int      `json:"unresolved"`
	Exempt           int      `json:"exempt"`
	PercentSatisfied float64  `json:"percent_satisfied"`
}

// ScenarioTypeSummary is the per-depth-scenario coverage tally. It lets the C-7
// dashboard show depth coverage separately from breadth/axis coverage.
type ScenarioTypeSummary struct {
	ScenarioType     string  `json:"scenario_type"`
	Total            int     `json:"total"`
	Covered          int     `json:"covered"`
	Uncovered        int     `json:"uncovered"`
	Unresolved       int     `json:"unresolved"`
	Exempt           int     `json:"exempt"`
	PercentSatisfied float64 `json:"percent_satisfied"`
}

// SurfaceReport is the report row for one supported surface.
type SurfaceReport struct {
	Registry     Registry `json:"registry"`
	Key          string   `json:"key"`
	ScenarioType string   `json:"scenario_type"`
	Status       Status   `json:"status"`
	Scenario     string   `json:"scenario,omitempty"`
	Ref          string   `json:"ref,omitempty"`
	ProofGate    string   `json:"proof_gate,omitempty"`
	Detail       string   `json:"detail"`
}

// CoverageReport is the machine-readable coverage artifact emitted on every gate
// run. It is the C-7 dashboard input: per-registry percentages, the gap list, and
// the full per-surface breakdown.
type CoverageReport struct {
	SchemaVersion string            `json:"schema_version"`
	Blocking      bool              `json:"blocking"`
	Totals        RegistrySummary   `json:"totals"`
	Summaries     []RegistrySummary `json:"registry_summaries"`
	// ScenarioTypeSummaries are per-depth tallies for C-8 coverage.
	ScenarioTypeSummaries []ScenarioTypeSummary `json:"scenario_type_summaries"`
	Surfaces              []SurfaceReport       `json:"surfaces"`
	// Gaps are the uncovered and unresolved surface keys (the actionable
	// worklist), sorted.
	Gaps []string `json:"gaps"`
	// Stale are manifest surfaces matching no supported surface, sorted.
	Stale []string `json:"stale_manifest_surfaces"`
	// LanguageScoreboard is the C-11 (#4364) language-parser coverage scoreboard:
	// the honest count of how many ledger languages the golden-corpus corpus
	// exercises, plus the explicit uncovered list (the C-12 #4365 worklist). It is
	// visibility-only and does not affect the blocking surface reconcile above.
	LanguageScoreboard LanguageScoreboard `json:"language_scoreboard"`
}

// BuildReport renders a reconciliation into the coverage-report artifact.
func BuildReport(c Coverage, blocking bool) CoverageReport {
	rep := CoverageReport{
		SchemaVersion: ReportSchemaVersion,
		Blocking:      blocking,
		Stale:         append([]string(nil), c.Stale...),
	}

	perRegistry := map[Registry]*RegistrySummary{}
	for _, reg := range allRegistries {
		perRegistry[reg] = &RegistrySummary{Registry: reg}
	}
	perScenarioType := map[string]*ScenarioTypeSummary{}

	for _, sc := range c.Surfaces {
		sum := perRegistry[sc.Surface.Registry]
		if sum == nil {
			sum = &RegistrySummary{Registry: sc.Surface.Registry}
			perRegistry[sc.Surface.Registry] = sum
		}
		tally(sum, sc.Status)
		tally(&rep.Totals, sc.Status)
		depthType := surfaceCoverageScenarioType(sc)
		scenarioType := string(depthType)
		typeSum := perScenarioType[scenarioType]
		if typeSum == nil {
			typeSum = &ScenarioTypeSummary{ScenarioType: scenarioType}
			perScenarioType[scenarioType] = typeSum
		}
		tallyScenarioType(typeSum, sc.Status)

		row := SurfaceReport{
			Registry:     sc.Surface.Registry,
			Key:          sc.Surface.Key,
			ScenarioType: scenarioType,
			Status:       sc.Status,
			Detail:       sc.Detail,
		}
		if sc.Scenario != nil {
			row.Scenario = string(sc.Scenario.Scenario)
			row.Ref = sc.Scenario.Ref
			row.ProofGate = sc.Scenario.ProofGate
		}
		rep.Surfaces = append(rep.Surfaces, row)

		if sc.Status == StatusUncovered || sc.Status == StatusUnresolved {
			rep.Gaps = append(rep.Gaps, coverageDisplayKey(sc.Surface.Key, depthType))
		}
	}

	for _, reg := range reportRegistryOrder(perRegistry) {
		sum := perRegistry[reg]
		finalizePercent(sum)
		rep.Summaries = append(rep.Summaries, *sum)
	}
	for _, sum := range perScenarioType {
		finalizeScenarioTypePercent(sum)
		rep.ScenarioTypeSummaries = append(rep.ScenarioTypeSummaries, *sum)
	}
	sort.Slice(rep.ScenarioTypeSummaries, func(i, j int) bool {
		return rep.ScenarioTypeSummaries[i].ScenarioType < rep.ScenarioTypeSummaries[j].ScenarioType
	})
	finalizePercent(&rep.Totals)
	sort.Strings(rep.Gaps)
	return rep
}

// reportRegistryOrder returns the registries to emit summaries for, sorted
// alphabetically. perRegistry is pre-seeded with every base (breadth) registry —
// so those always appear even with zero surfaces — and additionally holds any
// depth-applicability registry that actually has surfaces (C-13). A depth
// registry with no surfaces (depth derivation off) is absent and not shown, so
// pre-C-13 report output is unchanged.
func reportRegistryOrder(perRegistry map[Registry]*RegistrySummary) []Registry {
	out := make([]Registry, 0, len(perRegistry))
	for reg := range perRegistry {
		out = append(out, reg)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func surfaceCoverageScenarioType(sc SurfaceCoverage) DepthScenarioType {
	if sc.ScenarioType != "" {
		return sc.ScenarioType
	}
	if sc.Scenario != nil && sc.Scenario.ScenarioType != "" {
		return sc.Scenario.ScenarioType
	}
	return ScenarioTypeBaseline
}

func tallyScenarioType(sum *ScenarioTypeSummary, status Status) {
	sum.Total++
	switch status {
	case StatusCovered:
		sum.Covered++
	case StatusUncovered:
		sum.Uncovered++
	case StatusUnresolved:
		sum.Unresolved++
	case StatusExempt:
		sum.Exempt++
	}
}

func tally(sum *RegistrySummary, status Status) {
	sum.Total++
	switch status {
	case StatusCovered:
		sum.Covered++
	case StatusUncovered:
		sum.Uncovered++
	case StatusUnresolved:
		sum.Unresolved++
	case StatusExempt:
		sum.Exempt++
	}
}

// finalizePercent computes the satisfied percentage (covered + exempt over total)
// rounded to two decimals. A registry with no surfaces is reported as 100% so an
// empty registry never drags the dashboard down with a false 0.
func finalizePercent(sum *RegistrySummary) {
	if sum.Total == 0 {
		sum.PercentSatisfied = 100
		return
	}
	satisfied := float64(sum.Covered + sum.Exempt)
	sum.PercentSatisfied = float64(int((satisfied/float64(sum.Total))*10000+0.5)) / 100
}

func finalizeScenarioTypePercent(sum *ScenarioTypeSummary) {
	if sum.Total == 0 {
		sum.PercentSatisfied = 100
		return
	}
	satisfied := float64(sum.Covered + sum.Exempt)
	sum.PercentSatisfied = float64(int((satisfied/float64(sum.Total))*10000+0.5)) / 100
}

// MarshalReport renders the report as deterministic, indented JSON with a
// trailing newline, suitable for writing as the CI artifact.
func MarshalReport(rep CoverageReport) ([]byte, error) {
	payload, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal coverage report: %w", err)
	}
	return append(payload, '\n'), nil
}
