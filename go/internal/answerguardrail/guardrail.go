// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package answerguardrail

import (
	"regexp"
	"strings"
)

// Criterion identifies one runtime answer guardrail.
type Criterion string

const (
	// CriterionCitationCoverage requires supported published answers to carry
	// at least one citation or evidence handle.
	CriterionCitationCoverage Criterion = "citation_coverage"
	// CriterionPublishSafety rejects publishable strings that look like private
	// paths, hosts, credentials, or raw addresses.
	CriterionPublishSafety Criterion = "publish_safety"
	// CriterionAnswerSubstance rejects a tautological, identity-only answer that
	// only restates the question's entity and carries no operational fact, even
	// when citation and publish-safety pass.
	CriterionAnswerSubstance Criterion = "answer_substance"
)

// Result is the bounded answer shape evaluated by guardrails.
type Result struct {
	// Question is the original question the answer addresses. It is used only by
	// the answer-substance check to detect a circular, identity-only answer; an
	// empty Question disables that check.
	Question      string
	AnswerSummary string
	Supported     bool
	// CitationHandles are inlined evidence handles that cover the prose.
	CitationHandles []string
	// TruthProvenance reports whether the prose is backed by a classified
	// packet's truth provenance (a non-empty truth_class). It is a recognized
	// citation_coverage class on par with citation handles, mirroring the
	// narration validator, which allows ProvenanceTruth whenever the packet's
	// truth_class is non-empty. It does not weaken coverage for genuinely uncited
	// prose: prose with no handles and no truth provenance is still blocked
	// (issue #3609).
	TruthProvenance bool
	Limitations     []string
	NextCalls       []string
	Metadata        []string
}

// Finding describes one failed guardrail without echoing the unsafe value.
type Finding struct {
	Criterion Criterion
	Detail    string
}

// Verdict is the aggregate guardrail result.
type Verdict struct {
	Valid    bool
	Findings []Finding
}

var rawAddressPattern = regexp.MustCompile(`\b[0-9]{1,3}(?:\.[0-9]{1,3}){3}\b`)

// ValidateResult evaluates result against runtime-safe citation and publish
// safety rules. It performs no I/O and never calls providers.
func ValidateResult(result Result) Verdict {
	var findings []Finding
	if result.Supported && strings.TrimSpace(result.AnswerSummary) != "" && !hasCoverage(result) {
		findings = append(findings, Finding{
			Criterion: CriterionCitationCoverage,
			Detail:    "supported published answer has no citation handles or truth provenance",
		})
	}
	if FirstUnsafeString(result.Strings()) != "" {
		findings = append(findings, Finding{
			Criterion: CriterionPublishSafety,
			Detail:    "publishable answer contains a restricted private or credential-like value",
		})
	}
	if result.Supported && strings.TrimSpace(result.Question) != "" &&
		IsCircularAnswer(result.Question, result.AnswerSummary) {
		findings = append(findings, Finding{
			Criterion: CriterionAnswerSubstance,
			Detail:    "supported answer is circular or identity-only and names no operational fact",
		})
	}
	return Verdict{
		Valid:    len(findings) == 0,
		Findings: findings,
	}
}

// HasFinding reports whether the verdict contains criterion.
func (v Verdict) HasFinding(criterion Criterion) bool {
	for _, finding := range v.Findings {
		if finding.Criterion == criterion {
			return true
		}
	}
	return false
}

// Strings returns every publishable string carried by result.
func (r Result) Strings() []string {
	values := []string{r.AnswerSummary}
	values = append(values, r.CitationHandles...)
	values = append(values, r.Limitations...)
	values = append(values, r.NextCalls...)
	values = append(values, r.Metadata...)
	return values
}

// FirstUnsafeString returns the first value rejected by the publish-safety
// scanner, or the empty string when all values are publish-safe.
func FirstUnsafeString(values []string) string {
	for _, value := range values {
		if UnsafeString(value) {
			return value
		}
	}
	return ""
}

// UnsafeString reports whether value looks unsafe for publishable answer
// output. It is intentionally conservative and deterministic.
func UnsafeString(value string) bool {
	lower := strings.ToLower(value)
	if rawAddressPattern.MatchString(value) {
		return true
	}
	for _, fragment := range []string{
		"/users/",
		"/home/",
		"\\users\\",
		"bearer ",
		"password=",
		"token=",
		"secret=",
		"api_key=",
		"api-key=",
		".internal",
		".corp",
		".local",
	} {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return strings.Contains(lower, "http://") || strings.Contains(lower, "https://")
}

func hasCitation(handles []string) bool {
	for _, handle := range handles {
		if strings.TrimSpace(handle) != "" {
			return true
		}
	}
	return false
}

// hasCoverage reports whether a supported published answer carries an accepted
// citation_coverage class: inlined evidence handles OR truth provenance (a
// classified packet's non-empty truth_class). Truth provenance is recognized on
// par with citation handles, mirroring the narration validator's ProvenanceTruth
// allowance. Prose with neither is uncovered and still fails the criterion
// (issue #3609).
func hasCoverage(result Result) bool {
	return hasCitation(result.CitationHandles) || result.TruthProvenance
}
