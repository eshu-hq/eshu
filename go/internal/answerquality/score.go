// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package answerquality

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/answerguardrail"
)

// ParseEvidence decodes and version-checks a scorecard evidence payload.
func ParseEvidence(raw []byte) (Evidence, error) {
	var evidence Evidence
	if err := json.Unmarshal(raw, &evidence); err != nil {
		return Evidence{}, fmt.Errorf("decode answer-quality evidence: %w", err)
	}
	if evidence.Version != EvidenceVersion {
		return Evidence{}, fmt.Errorf("version must be %s", EvidenceVersion)
	}
	return evidence, nil
}

// Score evaluates captured evidence against the default scorecard suite.
func Score(evidence Evidence) Verdict {
	verdict := Verdict{
		Version: EvidenceVersion,
		RunID:   evidence.RunID,
		Pass:    true,
	}
	verdict.Criteria = append(verdict.Criteria, scoreFamilyCoverage(evidence))
	for _, prompt := range evidence.Prompts {
		score := scorePrompt(prompt)
		verdict.PromptScores = append(verdict.PromptScores, score)
		for _, criterion := range score.Criteria {
			if criterion.Status == CriterionFail {
				verdict.FollowUpIssues = append(verdict.FollowUpIssues, followUpFor(prompt, criterion))
			}
		}
	}
	for _, name := range []CriterionName{
		CriterionUsefulness,
		CriterionTruthHonesty,
		CriterionCitationCoverage,
		CriterionBoundedness,
		CriterionNarrationFallback,
		CriterionParity,
		CriterionFollowUpUsefulness,
	} {
		verdict.Criteria = append(verdict.Criteria, aggregatePromptCriterion(name, verdict.PromptScores))
	}
	verdict.Criteria = append(verdict.Criteria, aggregatePublishSafety(evidence, verdict.PromptScores))
	for _, criterion := range verdict.Criteria {
		if criterion.Status == CriterionFail {
			verdict.Pass = false
			if criterion.Name == CriterionFamilyCoverage {
				verdict.FollowUpIssues = append(verdict.FollowUpIssues, FollowUpIssue{
					Title:  "Fill answer-quality scorecard family coverage",
					Labels: []string{"capability:answer-experience", "answer:dogfood"},
					Detail: criterion.Detail,
				})
			}
		}
	}
	verdict.Score = scorePercent(verdict.Criteria)
	return verdict
}

func aggregatePublishSafety(evidence Evidence, scores []PromptScore) CriterionScore {
	if unsafe := answerguardrail.FirstUnsafeString([]string{evidence.RunID, evidence.EshuCommit}); unsafe != "" {
		return fail(CriterionPublishSafety, "unsafe run metadata: "+unsafe)
	}
	return aggregatePromptCriterion(CriterionPublishSafety, scores)
}

func scorePrompt(prompt PromptResult) PromptScore {
	score := PromptScore{ID: prompt.ID, Family: prompt.Family, Pass: true}
	score.Criteria = append(
		score.Criteria,
		scoreUsefulness(prompt),
		scoreTruthHonesty(prompt),
		scoreCitationCoverage(prompt),
		scoreBoundedness(prompt),
		scoreNarrationFallback(prompt),
		scoreParity(prompt),
		scoreFollowUpUsefulness(prompt),
		scorePublishSafety(prompt),
	)
	for _, criterion := range score.Criteria {
		if criterion.Status == CriterionFail {
			score.Pass = false
			break
		}
	}
	return score
}

func scoreFamilyCoverage(evidence Evidence) CriterionScore {
	present := map[PromptFamily]struct{}{}
	for _, prompt := range evidence.Prompts {
		present[prompt.Family] = struct{}{}
	}
	var missing []string
	for _, prompt := range DefaultSuite().Prompts {
		if _, ok := present[prompt.Family]; !ok {
			missing = append(missing, string(prompt.Family))
		}
	}
	if len(missing) > 0 {
		return CriterionScore{
			Name:   CriterionFamilyCoverage,
			Status: CriterionFail,
			Detail: "missing prompt families: " + strings.Join(missing, ", "),
		}
	}
	return CriterionScore{Name: CriterionFamilyCoverage, Status: CriterionPass, Detail: "all major answer families captured"}
}

func scoreUsefulness(prompt PromptResult) CriterionScore {
	if len(prompt.Results) == 0 {
		return fail(CriterionUsefulness, "no surface results captured")
	}
	for _, result := range prompt.Results {
		if !result.Useful || !result.Supported {
			return fail(CriterionUsefulness, fmt.Sprintf("%s result was not useful or supported", result.Surface))
		}
		if result.TooGeneric || result.TooVerbose {
			return fail(CriterionUsefulness, fmt.Sprintf("%s result was generic or too verbose", result.Surface))
		}
		if strings.TrimSpace(result.AnswerSummary) == "" {
			return fail(CriterionUsefulness, fmt.Sprintf("%s result had no answer summary", result.Surface))
		}
	}
	return pass(CriterionUsefulness, "captured answers are useful and supported")
}

func scoreTruthHonesty(prompt PromptResult) CriterionScore {
	if len(prompt.Results) == 0 {
		return fail(CriterionTruthHonesty, "no surface results captured")
	}
	wantTruth := expectedTruth(prompt)
	for _, result := range prompt.Results {
		if strings.TrimSpace(result.TruthClass) == "" {
			return fail(CriterionTruthHonesty, fmt.Sprintf("%s result missing truth class", result.Surface))
		}
		if wantTruth != "" && result.TruthClass != wantTruth {
			return fail(CriterionTruthHonesty, fmt.Sprintf("%s result truth %q, want %q", result.Surface, result.TruthClass, wantTruth))
		}
		if result.StaleNoCause || result.OverConfident {
			return fail(CriterionTruthHonesty, fmt.Sprintf("%s result was stale without cause or over-confident", result.Surface))
		}
		if strings.TrimSpace(result.Freshness) == "stale" && len(result.Limitations) == 0 {
			return fail(CriterionTruthHonesty, fmt.Sprintf("%s result was stale without a limitation", result.Surface))
		}
	}
	return pass(CriterionTruthHonesty, "truth classes and freshness are honest")
}

func scoreCitationCoverage(prompt PromptResult) CriterionScore {
	if len(prompt.Results) == 0 {
		return fail(CriterionCitationCoverage, "no surface results captured")
	}
	for _, result := range prompt.Results {
		verdict := answerguardrail.ValidateResult(answerguardrail.Result{
			AnswerSummary:   result.AnswerSummary,
			Supported:       result.Supported,
			CitationHandles: result.CitationHandles,
			// Mirror the runtime Ask handler: a classified packet's truth
			// provenance (non-empty truth_class) is an accepted citation_coverage
			// class, so offline CI scoring and runtime publishing share the same
			// guardrail semantics (#3609).
			TruthProvenance: strings.TrimSpace(result.TruthClass) != "",
		})
		if verdict.HasFinding(answerguardrail.CriterionCitationCoverage) {
			return fail(CriterionCitationCoverage, fmt.Sprintf("%s result has no citation handles", result.Surface))
		}
	}
	return pass(CriterionCitationCoverage, "all captured results include citation handles")
}

func scoreBoundedness(prompt PromptResult) CriterionScore {
	if len(prompt.Results) == 0 {
		return fail(CriterionBoundedness, "no surface results captured")
	}
	for _, result := range prompt.Results {
		if (result.Partial || result.Truncated) && len(result.Limitations) == 0 && len(result.NextCalls) == 0 {
			return fail(CriterionBoundedness, fmt.Sprintf("%s partial/truncated result has no limitation or continuation", result.Surface))
		}
		if !result.Supported && len(result.Limitations) == 0 {
			return fail(CriterionBoundedness, fmt.Sprintf("%s unsupported result has no limitation", result.Surface))
		}
	}
	return pass(CriterionBoundedness, "partial and unsupported states are bounded")
}

func scoreParity(prompt PromptResult) CriterionScore {
	required := requiredSurfaces(prompt)
	seen := map[Surface]string{}
	for _, result := range prompt.Results {
		seen[result.Surface] = result.TruthClass
	}
	var missing []string
	for _, surface := range required {
		if _, ok := seen[surface]; !ok {
			missing = append(missing, string(surface))
		}
	}
	if len(missing) > 0 {
		return fail(CriterionParity, "missing required surfaces: "+strings.Join(missing, ", "))
	}
	var truthClass string
	for _, surface := range required {
		if truthClass == "" {
			truthClass = seen[surface]
			continue
		}
		if seen[surface] != truthClass {
			return fail(CriterionParity, "required surfaces returned different truth classes")
		}
	}
	return pass(CriterionParity, "required surfaces are present and truth classes agree")
}

func scoreFollowUpUsefulness(prompt PromptResult) CriterionScore {
	if len(prompt.Results) == 0 {
		return fail(CriterionFollowUpUsefulness, "no surface results captured")
	}
	required := requiredNextCalls(prompt)
	combined := map[string]struct{}{}
	for _, result := range prompt.Results {
		if result.MissingFollowUp {
			return fail(CriterionFollowUpUsefulness, fmt.Sprintf("%s result marked missing follow-up", result.Surface))
		}
		for _, next := range result.NextCalls {
			combined[next] = struct{}{}
		}
		if (result.Partial || result.Truncated) && len(result.NextCalls) == 0 {
			return fail(CriterionFollowUpUsefulness, fmt.Sprintf("%s partial/truncated result has no next call", result.Surface))
		}
	}
	var missing []string
	for _, next := range required {
		if _, ok := combined[next]; !ok {
			missing = append(missing, next)
		}
	}
	if len(missing) > 0 {
		return fail(CriterionFollowUpUsefulness, "missing required next calls: "+strings.Join(missing, ", "))
	}
	return pass(CriterionFollowUpUsefulness, "required next calls are present")
}

func scorePublishSafety(prompt PromptResult) CriterionScore {
	if unsafe := answerguardrail.FirstUnsafeString(promptStrings(prompt)); unsafe != "" {
		return fail(CriterionPublishSafety, "unsafe publishable evidence: "+unsafe)
	}
	return pass(CriterionPublishSafety, "evidence contains only redacted publishable strings")
}

func aggregatePromptCriterion(name CriterionName, scores []PromptScore) CriterionScore {
	if len(scores) == 0 {
		return fail(name, "no prompts captured")
	}
	for _, score := range scores {
		for _, criterion := range score.Criteria {
			if criterion.Name == name && criterion.Status == CriterionFail {
				return fail(name, fmt.Sprintf("%s: %s", score.Family, criterion.Detail))
			}
		}
	}
	return pass(name, "all captured prompts passed")
}

func scorePercent(criteria []CriterionScore) int {
	if len(criteria) == 0 {
		return 0
	}
	passed := 0
	for _, criterion := range criteria {
		if criterion.Status == CriterionPass {
			passed++
		}
	}
	return passed * 100 / len(criteria)
}

func fail(name CriterionName, detail string) CriterionScore {
	return CriterionScore{Name: name, Status: CriterionFail, Detail: detail}
}

func pass(name CriterionName, detail string) CriterionScore {
	return CriterionScore{Name: name, Status: CriterionPass, Detail: detail}
}

func expectedTruth(prompt PromptResult) string {
	if prompt.ExpectedTruthClass != "" {
		return prompt.ExpectedTruthClass
	}
	if spec, ok := DefaultSuite().PromptByFamily(prompt.Family); ok {
		return spec.ExpectedTruthClass
	}
	return ""
}

func requiredSurfaces(prompt PromptResult) []Surface {
	if len(prompt.RequiredSurfaces) > 0 {
		return prompt.RequiredSurfaces
	}
	if spec, ok := DefaultSuite().PromptByFamily(prompt.Family); ok {
		return spec.RequiredSurfaces
	}
	return nil
}

func requiredNextCalls(prompt PromptResult) []string {
	if len(prompt.RequiredNextCalls) > 0 {
		return prompt.RequiredNextCalls
	}
	if spec, ok := DefaultSuite().PromptByFamily(prompt.Family); ok {
		return spec.RequiredNextCalls
	}
	return nil
}

func promptStrings(prompt PromptResult) []string {
	values := []string{prompt.ID, string(prompt.Family), prompt.Prompt, prompt.ExpectedTruthClass}
	values = append(values, prompt.RequiredNextCalls...)
	values = append(values, prompt.AcceptableLimitations...)
	for _, result := range prompt.Results {
		values = append(
			values,
			string(result.Surface),
			result.AnswerSummary,
			result.TruthClass,
			result.Freshness,
		)
		values = append(values, result.CitationHandles...)
		values = append(values, result.Limitations...)
		values = append(values, result.NextCalls...)
		values = append(values, narrationStrings(result.Narration)...)
	}
	return values
}

func followUpFor(prompt PromptResult, criterion CriterionScore) FollowUpIssue {
	labels := []string{"capability:answer-experience", "answer:dogfood"}
	switch criterion.Name {
	case CriterionCitationCoverage:
		labels = append(labels, "answer:citations")
	case CriterionNarrationFallback:
		labels = append(labels, "answer:narration")
	case CriterionParity:
		labels = append(labels, "answer:parity")
	case CriterionTruthHonesty, CriterionFollowUpUsefulness:
		labels = append(labels, "answer:mcp")
	}
	switch prompt.Family {
	case PromptFamilyDocumentationTruth:
		labels = append(labels, "answer:docs")
	case PromptFamilyHostedGovernance:
		labels = append(labels, "capability:hosted-ops")
	}
	slices.Sort(labels)
	labels = slices.Compact(labels)
	return FollowUpIssue{
		Title:  fmt.Sprintf("Fix %s answer-quality score for %s", criterion.Name, prompt.Family),
		Labels: labels,
		Detail: criterion.Detail,
	}
}
