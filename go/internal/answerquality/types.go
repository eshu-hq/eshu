// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package answerquality defines the publish-safe answer dogfood scorecard.
package answerquality

import "github.com/eshu-hq/eshu/go/internal/answernarration"

// EvidenceVersion is the current answer-quality scorecard evidence schema.
const EvidenceVersion = "answer-quality-scorecard/v1"

// PromptFamily identifies one representative answer workflow family.
type PromptFamily string

const (
	// PromptFamilyServiceStory scores service story and citation answers.
	PromptFamilyServiceStory PromptFamily = "service_story"
	// PromptFamilyCodeTopic scores code-topic investigation answers.
	PromptFamilyCodeTopic PromptFamily = "code_topic"
	// PromptFamilyIncidentContext scores incident-context answers.
	PromptFamilyIncidentContext PromptFamily = "incident_context"
	// PromptFamilySupplyChainImpact scores supply-chain impact answers.
	PromptFamilySupplyChainImpact PromptFamily = "supply_chain_impact"
	// PromptFamilyDocumentationTruth scores documentation truth answers.
	PromptFamilyDocumentationTruth PromptFamily = "documentation_truth"
	// PromptFamilyFreshnessReadiness scores freshness and readiness answers.
	PromptFamilyFreshnessReadiness PromptFamily = "freshness_readiness"
	// PromptFamilyHostedGovernance scores hosted onboarding and governance answers.
	PromptFamilyHostedGovernance PromptFamily = "hosted_onboarding_governance"
)

// Surface identifies the answer surface that produced one captured result.
type Surface string

const (
	// SurfaceAPI is the HTTP API answer surface.
	SurfaceAPI Surface = "api"
	// SurfaceMCP is the MCP tool answer surface.
	SurfaceMCP Surface = "mcp"
	// SurfaceCLI is the CLI answer surface.
	SurfaceCLI Surface = "cli"
	// SurfaceHosted is a deployed hosted-service surface.
	SurfaceHosted Surface = "hosted"
)

// NarrationStatus records whether optional governed narration was used.
type NarrationStatus string

const (
	// NarrationStatusNotRequested means no optional narration was attempted.
	NarrationStatusNotRequested NarrationStatus = "not_requested"
	// NarrationStatusAccepted means governed narration was accepted for review.
	NarrationStatusAccepted NarrationStatus = "accepted"
	// NarrationStatusRejected means narration was rejected and fallback won.
	NarrationStatusRejected NarrationStatus = "rejected"
	// NarrationStatusUnavailable means narration was unavailable and fallback won.
	NarrationStatusUnavailable NarrationStatus = "unavailable"
)

// CriterionName identifies one scored answer-quality criterion.
type CriterionName string

const (
	// CriterionFamilyCoverage proves every major answer family was captured.
	CriterionFamilyCoverage CriterionName = "family_coverage"
	// CriterionUsefulness rejects generic, unsupported, or too-verbose answers.
	CriterionUsefulness CriterionName = "usefulness"
	// CriterionTruthHonesty rejects stale, missing, or over-confident truth.
	CriterionTruthHonesty CriterionName = "truth_honesty"
	// CriterionCitationCoverage requires concrete evidence handles.
	CriterionCitationCoverage CriterionName = "citation_coverage"
	// CriterionBoundedness requires partial/truncated answers to say so.
	CriterionBoundedness CriterionName = "boundedness"
	// CriterionNarrationFallback rejects narrated presentation that weakens the
	// deterministic fallback answer row.
	CriterionNarrationFallback CriterionName = "narration_fallback"
	// CriterionParity requires the expected surfaces to agree.
	CriterionParity CriterionName = "parity"
	// CriterionFollowUpUsefulness requires actionable next calls.
	CriterionFollowUpUsefulness CriterionName = "follow_up_usefulness"
	// CriterionPublishSafety rejects private paths, hosts, addresses, or secrets.
	CriterionPublishSafety CriterionName = "publish_safety"
	// CriterionTruthClassPreservation rejects a report that upgrades or invents a
	// section or report-level truth class beyond its source.
	CriterionTruthClassPreservation CriterionName = "truth_class_preservation"
	// CriterionLimitationVisibility rejects a partial or unsupported section that
	// hides why it is incomplete.
	CriterionLimitationVisibility CriterionName = "limitation_visibility"
	// CriterionNextCallExecutability rejects a recommended next call or suggested
	// investigation that names no executable tool, route, or playbook.
	CriterionNextCallExecutability CriterionName = "next_call_executability"
	// CriterionTruncationSignaling rejects a report that truncates evidence
	// without marking the section partial and saying so.
	CriterionTruncationSignaling CriterionName = "truncation_signaling"
	// CriterionUnsupportedClaimAvoidance rejects a confident summary on an
	// unsupported or evidence-less section.
	CriterionUnsupportedClaimAvoidance CriterionName = "unsupported_claim_avoidance"
)

// ReportEvidenceVersion is the service intelligence report scorecard schema.
const ReportEvidenceVersion = "service-intelligence-report-scorecard/v1"

// CriterionStatus is the outcome of a scored criterion.
type CriterionStatus string

const (
	// CriterionPass means the criterion passed.
	CriterionPass CriterionStatus = "pass"
	// CriterionFail means the criterion failed and rejects the scorecard.
	CriterionFail CriterionStatus = "fail"
	// CriterionNotMeasured records an honest gap without fabricating proof.
	CriterionNotMeasured CriterionStatus = "not_measured"
)

// Evidence is one captured answer-quality dogfood run.
type Evidence struct {
	Version    string         `json:"version"`
	RunID      string         `json:"run_id,omitempty"`
	EshuCommit string         `json:"eshu_commit,omitempty"`
	Prompts    []PromptResult `json:"prompts"`
}

// PromptSpec defines the default representative prompt suite.
type PromptSpec struct {
	ID                 string       `json:"id"`
	Family             PromptFamily `json:"family"`
	Prompt             string       `json:"prompt"`
	ExpectedTruthClass string       `json:"expected_truth_class"`
	RequiredSurfaces   []Surface    `json:"required_surfaces"`
	RequiredNextCalls  []string     `json:"required_next_calls"`
}

// Suite is the canonical list of prompt families expected in one scorecard.
type Suite struct {
	Prompts []PromptSpec `json:"prompts"`
}

// PromptByFamily returns the prompt spec for a family.
func (s Suite) PromptByFamily(family PromptFamily) (PromptSpec, bool) {
	for _, prompt := range s.Prompts {
		if prompt.Family == family {
			return prompt, true
		}
	}
	return PromptSpec{}, false
}

// PromptResult is the captured scorecard row for one representative prompt.
type PromptResult struct {
	ID                    string          `json:"id"`
	Family                PromptFamily    `json:"family"`
	Prompt                string          `json:"prompt"`
	ExpectedTruthClass    string          `json:"expected_truth_class"`
	RequiredSurfaces      []Surface       `json:"required_surfaces"`
	RequiredNextCalls     []string        `json:"required_next_calls,omitempty"`
	AcceptableLimitations []string        `json:"acceptable_limitations,omitempty"`
	Results               []SurfaceResult `json:"results"`
}

// SurfaceResult is one captured answer from API, MCP, CLI, or hosted mode.
type SurfaceResult struct {
	Surface         Surface              `json:"surface"`
	Useful          bool                 `json:"useful"`
	Supported       bool                 `json:"supported"`
	AnswerSummary   string               `json:"answer_summary"`
	TruthClass      string               `json:"truth_class"`
	Freshness       string               `json:"freshness"`
	Partial         bool                 `json:"partial,omitempty"`
	Truncated       bool                 `json:"truncated,omitempty"`
	CitationHandles []string             `json:"citation_handles,omitempty"`
	Limitations     []string             `json:"limitations,omitempty"`
	NextCalls       []string             `json:"next_calls,omitempty"`
	TooGeneric      bool                 `json:"too_generic,omitempty"`
	StaleNoCause    bool                 `json:"stale_without_cause,omitempty"`
	OverConfident   bool                 `json:"over_confident,omitempty"`
	TooVerbose      bool                 `json:"too_verbose,omitempty"`
	MissingFollowUp bool                 `json:"missing_follow_up,omitempty"`
	Narration       *NarrationComparison `json:"narration,omitempty"`
}

// NarrationComparison compares optional narration with deterministic fallback.
type NarrationComparison struct {
	Status         NarrationStatus        `json:"status"`
	FallbackRef    string                 `json:"fallback_ref,omitempty"`
	Fallback       NarrationBaseline      `json:"fallback"`
	ValidatorInput *answernarration.Input `json:"validator_input,omitempty"`
}

// NarrationBaseline is the deterministic row optional narration must preserve.
type NarrationBaseline struct {
	Supported       bool     `json:"supported"`
	TruthClass      string   `json:"truth_class"`
	Freshness       string   `json:"freshness"`
	Partial         bool     `json:"partial,omitempty"`
	Truncated       bool     `json:"truncated,omitempty"`
	CitationHandles []string `json:"citation_handles,omitempty"`
	Limitations     []string `json:"limitations,omitempty"`
	NextCalls       []string `json:"next_calls,omitempty"`
}

// CriterionScore is one scored criterion in a prompt or whole-card verdict.
type CriterionScore struct {
	Name   CriterionName   `json:"name"`
	Status CriterionStatus `json:"status"`
	Detail string          `json:"detail,omitempty"`
}

// PromptScore is the per-prompt scorecard verdict.
type PromptScore struct {
	ID       string           `json:"id"`
	Family   PromptFamily     `json:"family"`
	Pass     bool             `json:"pass"`
	Criteria []CriterionScore `json:"criteria"`
}

// FollowUpIssue is an actionable issue suggestion for a failed scorecard row.
type FollowUpIssue struct {
	Title  string   `json:"title"`
	Labels []string `json:"labels"`
	Detail string   `json:"detail"`
}

// Verdict is the aggregate answer-quality scorecard result.
type Verdict struct {
	Version        string           `json:"version"`
	RunID          string           `json:"run_id,omitempty"`
	Pass           bool             `json:"pass"`
	Score          int              `json:"score"`
	Criteria       []CriterionScore `json:"criteria"`
	PromptScores   []PromptScore    `json:"prompt_scores"`
	FollowUpIssues []FollowUpIssue  `json:"follow_up_issues,omitempty"`
}

// Criterion returns the aggregate score for a criterion.
func (v Verdict) Criterion(name CriterionName) CriterionScore {
	for _, criterion := range v.Criteria {
		if criterion.Name == name {
			return criterion
		}
	}
	return CriterionScore{Name: name}
}
