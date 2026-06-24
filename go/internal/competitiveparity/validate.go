// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package competitiveparity

import (
	"fmt"
	"sort"
	"strings"
)

// Validate scores inventory against expectations and returns a deterministic
// parity report. Missing surfaces or required documentation terms fail the
// relevant surface family.
func Validate(inv Inventory, expectations []Expectation) Report {
	commandSet := stringSet(inv.Commands)
	routeSet := stringSet(inv.APIRoutes)
	toolSet := stringSet(inv.MCPTools)
	pageSet := stringSet(inv.ConsolePages)
	exerciseSet := exerciseResults(inv.Exercises)

	surfaces := make([]SurfaceResult, 0, len(expectations))
	for _, expectation := range expectations {
		result := SurfaceResult{
			ID:             expectation.ID,
			DisplayName:    expectation.DisplayName,
			PeerBaseline:   expectation.PeerBaseline,
			Pass:           true,
			PresencePass:   true,
			QualityPass:    true,
			RelatedIssues:  append([]IssueRef(nil), expectation.RelatedIssues...),
			ResidualIssues: append([]IssueRef(nil), expectation.ResidualIssues...),
		}
		result.Checks = append(result.Checks, checkMembership(CheckCLICommand, expectation.Commands, commandSet)...)
		result.Checks = append(result.Checks, checkMembership(CheckAPIRoute, expectation.APIRoutes, routeSet)...)
		result.Checks = append(result.Checks, checkMembership(CheckMCPTool, expectation.MCPTools, toolSet)...)
		result.Checks = append(result.Checks, checkMembership(CheckConsolePage, expectation.ConsolePages, pageSet)...)
		result.Checks = append(result.Checks, checkExercises(expectation.Exercises, exerciseSet)...)
		for _, doc := range expectation.Docs {
			result.Checks = append(result.Checks, checkDoc(inv.Docs, doc)...)
		}
		sortChecks(result.Checks)
		for _, check := range result.Checks {
			if check.Status == CheckFail {
				result.PresencePass = false
				break
			}
		}
		result.Quality = checkQuality(inv.Docs, expectation.Quality)
		sortQuality(result.Quality)
		result.QualityScore = scoreQuality(result.Quality)
		if result.QualityScore.Failed > 0 {
			result.QualityPass = false
		}
		result.Pass = result.PresencePass && result.QualityPass
		surfaces = append(surfaces, result)
	}
	sort.Slice(surfaces, func(i, j int) bool {
		return surfaces[i].ID < surfaces[j].ID
	})
	report := Report{
		SchemaVersion: SchemaVersion,
		Pass:          true,
		Surfaces:      surfaces,
	}
	report.Summary.SurfaceCount = len(surfaces)
	for _, surface := range surfaces {
		report.Summary.CheckCount += len(surface.Checks)
		if surface.Pass {
			report.Summary.Passed++
		} else {
			report.Pass = false
			report.Summary.Failed++
		}
	}
	return report
}

func checkQuality(docs map[string]string, expectations []QualityExpectation) []QualityResult {
	results := make([]QualityResult, 0, len(expectations))
	for _, expectation := range expectations {
		result := QualityResult{
			Dimension:   expectation.Dimension,
			DisplayName: expectation.DisplayName,
			Pass:        true,
			MaxScore:    len(expectation.Signals),
		}
		for _, signal := range expectation.Signals {
			if qualitySignalPresent(docs, signal) {
				result.Score++
				continue
			}
			result.Missing = append(result.Missing, signal)
		}
		minScore := expectation.MinScore
		if minScore <= 0 {
			minScore = result.MaxScore
		}
		if result.Score < minScore {
			result.Pass = false
		}
		result.Detail = fmt.Sprintf("matched %d/%d required usefulness signals", result.Score, result.MaxScore)
		results = append(results, result)
	}
	return results
}

func qualitySignalPresent(docs map[string]string, signal QualitySignal) bool {
	term := strings.TrimSpace(signal.Term)
	if term == "" {
		return true
	}
	if path := strings.TrimSpace(signal.SourcePath); path != "" {
		return strings.Contains(docs[path], term)
	}
	for _, body := range docs {
		if strings.Contains(body, term) {
			return true
		}
	}
	return false
}

func scoreQuality(results []QualityResult) QualityScore {
	score := QualityScore{}
	for _, result := range results {
		score.Score += result.Score
		score.Max += result.MaxScore
		if result.Pass {
			score.Passed++
		} else {
			score.Failed++
		}
	}
	return score
}

func checkExercises(targets []string, results map[string]ExerciseResult) []CheckResult {
	checks := make([]CheckResult, 0, len(targets))
	for _, target := range targets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		result, ok := results[target]
		check := CheckResult{Kind: CheckExercise, Target: target, Status: CheckPass, Detail: result.Detail}
		if !ok {
			check.Status = CheckFail
			check.Detail = fmt.Sprintf("%s was not exercised by the parity inventory", target)
		} else if !result.OK {
			check.Status = CheckFail
		}
		if strings.TrimSpace(check.Detail) == "" {
			check.Detail = "exercised"
		}
		checks = append(checks, check)
	}
	return checks
}

func checkMembership(kind CheckKind, targets []string, present map[string]struct{}) []CheckResult {
	checks := make([]CheckResult, 0, len(targets))
	for _, target := range targets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		check := CheckResult{Kind: kind, Target: target, Status: CheckPass, Detail: "present"}
		if _, ok := present[target]; !ok {
			check.Status = CheckFail
			check.Detail = fmt.Sprintf("%s is not present in the parity inventory", target)
		}
		checks = append(checks, check)
	}
	return checks
}

func checkDoc(docs map[string]string, doc DocExpectation) []CheckResult {
	var checks []CheckResult
	body, ok := docs[doc.Path]
	docCheck := CheckResult{Kind: CheckDoc, Target: doc.Path, Status: CheckPass, Detail: "present"}
	if !ok {
		docCheck.Status = CheckFail
		docCheck.Detail = "documentation path is missing from the parity inventory"
		checks = append(checks, docCheck)
		for _, term := range append(append([]string{}, doc.Terms...), doc.TruthTerms...) {
			checks = append(checks, CheckResult{
				Kind:   CheckTruthLabel,
				Target: term,
				Status: CheckFail,
				Detail: fmt.Sprintf("documentation path %s is missing", doc.Path),
			})
		}
		return checks
	}
	checks = append(checks, docCheck)
	for _, term := range doc.Terms {
		checks = append(checks, checkDocTerm(CheckDoc, doc.Path, body, term))
	}
	for _, term := range doc.TruthTerms {
		checks = append(checks, checkDocTerm(CheckTruthLabel, doc.Path, body, term))
	}
	return checks
}

func checkDocTerm(kind CheckKind, path string, body string, term string) CheckResult {
	status := CheckPass
	detail := fmt.Sprintf("%s contains %q", path, term)
	if !strings.Contains(body, term) {
		status = CheckFail
		detail = fmt.Sprintf("%s does not contain %q", path, term)
	}
	return CheckResult{Kind: kind, Target: term, Status: status, Detail: detail}
}

func stringSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}

func exerciseResults(results []ExerciseResult) map[string]ExerciseResult {
	out := make(map[string]ExerciseResult, len(results))
	for _, result := range results {
		id := strings.TrimSpace(result.ID)
		if id != "" {
			out[id] = result
		}
	}
	return out
}

func sortChecks(checks []CheckResult) {
	sort.SliceStable(checks, func(i, j int) bool {
		if checks[i].Kind != checks[j].Kind {
			return checks[i].Kind < checks[j].Kind
		}
		return checks[i].Target < checks[j].Target
	})
}

func sortQuality(results []QualityResult) {
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Dimension < results[j].Dimension
	})
}
