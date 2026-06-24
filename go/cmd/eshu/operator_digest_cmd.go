// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
)

const (
	operatorDigestSchema             = "operator_digest.v1"
	operatorDigestDefaultProfile     = "local_authoritative"
	operatorDigestDefaultQuestionMax = 8
	operatorDigestQuestionLimitMax   = 25
)

var operatorDigestSectionTemplates = []operatorDigestSectionTemplate{
	{ID: "hub_services", Title: "Hub Services"},
	{ID: "cross_domain_connections", Title: "Cross-Domain Connections"},
	{ID: "ambiguity_review_queue", Title: "Ambiguity Review Queue"},
	{ID: "freshness_and_drift", Title: "Freshness And Drift"},
	{ID: "unmanaged_or_orphaned_resources", Title: "Unmanaged Or Orphaned Resources"},
}

var operatorDigestQuestionTemplates = []operatorDigestQuestionTemplate{
	{
		SectionID:        "ambiguity_review_queue",
		Question:         "Which missing or ambiguous evidence should be resolved before acting on this scope?",
		Reason:           "missing_evidence_review",
		Target:           "query-playbook:documentation-truth",
		TruthExpectation: "semantic_observation",
	},
	{
		SectionID:        "freshness_and_drift",
		Question:         "Is the current scope fresh enough for an operator decision?",
		Reason:           "freshness_recovery",
		Target:           "route:/api/v0/status/pipeline",
		TruthExpectation: "deterministic",
	},
	{
		SectionID:        "hub_services",
		Question:         "Which service story should be built first for this scope?",
		Reason:           "hub_service_drilldown",
		Target:           "mcp:get_service_story",
		TruthExpectation: "deterministic",
	},
	{
		SectionID:        "cross_domain_connections",
		Question:         "Which code-to-cloud connection needs citation evidence for this scope?",
		Reason:           "cross_domain_citation",
		Target:           "mcp:build_evidence_citation_packet",
		TruthExpectation: "deterministic",
	},
	{
		SectionID:        "unmanaged_or_orphaned_resources",
		Question:         "Which runtime resource needs an ownership investigation for this scope?",
		Reason:           "ownership_investigation",
		Target:           "query-playbook:supply-chain-impact",
		TruthExpectation: "semantic_observation",
	},
}

func init() {
	rootCmd.AddCommand(newOperatorDigestCommand())
}

// newOperatorDigestCommand builds the deterministic operator digest renderer.
func newOperatorDigestCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Render a deterministic operator digest for an explicit scope",
		Long: `report renders the operator_digest.v1 model for an explicit scope.

This first CLI implementation is an offline presentation path. It validates
share-safe input, emits deterministic unsupported sections, and points operators
to bounded follow-up routes without reading graph state, writing graph state,
claiming reducer work, or calling providers.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runOperatorDigest,
	}
	cmd.Flags().String("scope", "", "Share-safe scope such as repo:owner/name, service:name, workload:name, environment:name, or project:name")
	cmd.Flags().String("profile", operatorDigestDefaultProfile, "Runtime profile used to derive the digest")
	cmd.Flags().Int("question-limit", operatorDigestDefaultQuestionMax, "Maximum suggested questions to emit (0-25)")
	cmd.Flags().Bool("json", false, "Emit the digest as JSON")
	cmd.Flags().String("artifact-out", "", "Write a shareable operator_digest_artifact.v1 JSON file")
	return cmd
}

func runOperatorDigest(cmd *cobra.Command, _ []string) error {
	rawScope, _ := cmd.Flags().GetString("scope")
	rawProfile, _ := cmd.Flags().GetString("profile")
	questionLimit, _ := cmd.Flags().GetInt("question-limit")
	jsonOut, _ := cmd.Flags().GetBool("json")
	artifactOut, _ := cmd.Flags().GetString("artifact-out")

	options, err := operatorDigestOptionsFromFlags(rawScope, rawProfile, questionLimit)
	if err != nil {
		return err
	}
	digest := buildOperatorDigest(options)
	if strings.TrimSpace(artifactOut) != "" {
		if err := writeOperatorDigestArtifact(artifactOut, digest); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "wrote operator digest artifact to %s\n", artifactOut)
	}
	if jsonOut {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if err := enc.Encode(digest); err != nil {
			return fmt.Errorf("write operator digest JSON: %w", err)
		}
		return nil
	}
	renderOperatorDigestText(cmd.OutOrStdout(), digest)
	return nil
}

type operatorDigestOptions struct {
	Scope         operatorDigestScope
	Profile       string
	QuestionLimit int
}

type operatorDigest struct {
	Schema             string                     `json:"schema"`
	Scope              operatorDigestScope        `json:"scope"`
	Profile            string                     `json:"profile"`
	Truth              operatorDigestTruth        `json:"truth"`
	Sections           []operatorDigestSection    `json:"sections"`
	SuggestedQuestions []operatorDigestQuestion   `json:"suggested_questions"`
	Limitations        []operatorDigestLimitation `json:"limitations"`
	SourceRefs         []operatorDigestSourceRef  `json:"source_refs"`
}

type operatorDigestScope struct {
	Type  string `json:"type"`
	Label string `json:"label"`
	ID    string `json:"id"`
}

type operatorDigestTruth struct {
	TruthClass string `json:"truth_class"`
	Freshness  string `json:"freshness"`
	Authority  string `json:"authority"`
	Reason     string `json:"reason"`
}

type operatorDigestSection struct {
	ID          string                     `json:"id"`
	Title       string                     `json:"title"`
	Status      string                     `json:"status"`
	Entries     []operatorDigestEntry      `json:"entries"`
	Limitations []operatorDigestLimitation `json:"limitations"`
	SourceRefs  []string                   `json:"source_refs"`
	Truncated   bool                       `json:"truncated"`
}

type operatorDigestEntry struct {
	ID string `json:"id"`
}

type operatorDigestQuestion struct {
	ID               string            `json:"id"`
	Question         string            `json:"question"`
	SourceSignal     string            `json:"source_signal"`
	Why              string            `json:"why"`
	Reason           string            `json:"reason"`
	Target           string            `json:"target"`
	Arguments        map[string]string `json:"arguments"`
	TruthExpectation string            `json:"truth_expectation"`
	EvidenceRefs     []string          `json:"evidence_refs"`
}

type operatorDigestLimitation struct {
	ID     string `json:"id"`
	Scope  string `json:"scope"`
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

type operatorDigestSourceRef struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type operatorDigestSectionTemplate struct {
	ID    string
	Title string
}

type operatorDigestQuestionTemplate struct {
	SectionID        string
	Question         string
	Reason           string
	Target           string
	TruthExpectation string
}

func operatorDigestOptionsFromFlags(rawScope, rawProfile string, questionLimit int) (operatorDigestOptions, error) {
	scope, err := normalizeOperatorDigestScope(rawScope)
	if err != nil {
		return operatorDigestOptions{}, err
	}
	profile := strings.TrimSpace(rawProfile)
	if profile == "" {
		return operatorDigestOptions{}, fmt.Errorf("profile is required")
	}
	if !isShareSafeOperatorDigestToken(profile) {
		return operatorDigestOptions{}, fmt.Errorf("profile must be share-safe")
	}
	if questionLimit < 0 || questionLimit > operatorDigestQuestionLimitMax {
		return operatorDigestOptions{}, fmt.Errorf("question-limit must be between 0 and 25")
	}
	return operatorDigestOptions{
		Scope:         scope,
		Profile:       profile,
		QuestionLimit: questionLimit,
	}, nil
}

func normalizeOperatorDigestScope(raw string) (operatorDigestScope, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return operatorDigestScope{}, fmt.Errorf("scope is required")
	}
	if strings.Contains(value, "://") || strings.HasPrefix(value, "/") || strings.HasPrefix(value, "~") || strings.Contains(value, "\\") {
		return operatorDigestScope{}, fmt.Errorf("scope must be share-safe")
	}
	scopeType := "repository"
	scopePrefix := "repo"
	label := value
	if prefix, rest, ok := strings.Cut(value, ":"); ok {
		scopePrefix = strings.TrimSpace(prefix)
		label = strings.TrimSpace(rest)
		mapped, ok := operatorDigestScopeTypes()[scopePrefix]
		if !ok {
			return operatorDigestScope{}, fmt.Errorf("unsupported scope type %q", scopePrefix)
		}
		scopeType = mapped
	}
	if label == "" {
		return operatorDigestScope{}, fmt.Errorf("scope is required")
	}
	if !isShareSafeOperatorDigestToken(scopePrefix) || !isShareSafeOperatorDigestLabel(label) {
		return operatorDigestScope{}, fmt.Errorf("scope must be share-safe")
	}
	return operatorDigestScope{
		Type:  scopeType,
		Label: label,
		ID:    scopePrefix + ":" + label,
	}, nil
}

func operatorDigestScopeTypes() map[string]string {
	return map[string]string{
		"repo":        "repository",
		"repository":  "repository",
		"service":     "service",
		"workload":    "workload",
		"environment": "environment",
		"project":     "project",
	}
}

func isShareSafeOperatorDigestLabel(value string) bool {
	if len(value) > 160 || strings.HasPrefix(value, "/") || strings.HasPrefix(value, "~") || strings.Contains(value, "..") || strings.Contains(value, ":") {
		return false
	}
	return isShareSafeOperatorDigestToken(value)
}

func isShareSafeOperatorDigestToken(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		switch r {
		case '.', '_', '-', '/':
			continue
		default:
			return false
		}
	}
	return true
}

func buildOperatorDigest(options operatorDigestOptions) operatorDigest {
	sourceRefs := []operatorDigestSourceRef{
		{ID: "cli:eshu-report", Kind: "cli_command", Name: "eshu report"},
		{ID: "doc:operator-digest-contract", Kind: "documentation", Name: "operator digest contract"},
		{ID: "profile:" + options.Profile, Kind: "runtime_profile", Name: options.Profile},
	}
	artifactLimitations := []operatorDigestLimitation{{
		ID:     "operator_digest.v1:limitation:read_surface:" + options.Scope.ID,
		Scope:  options.Scope.ID,
		Reason: "bounded_read_surface_not_connected",
		Detail: "The CLI digest renderer validates the contract but has not connected live bounded read surfaces in this slice.",
	}}
	sections := buildOperatorDigestSections(options.Scope)
	questions := buildOperatorDigestQuestions(options, sections)
	if len(questions) > options.QuestionLimit {
		questions = questions[:options.QuestionLimit]
		artifactLimitations = append(artifactLimitations, operatorDigestLimitation{
			ID:     "operator_digest.v1:limitation:suggested_questions:" + options.Scope.ID,
			Scope:  options.Scope.ID,
			Reason: "suggested_questions_truncated",
			Detail: "Suggested questions were truncated by the requested question-limit.",
		})
	}
	return operatorDigest{
		Schema:  operatorDigestSchema,
		Scope:   options.Scope,
		Profile: options.Profile,
		Truth: operatorDigestTruth{
			TruthClass: "unsupported",
			Freshness:  "unavailable",
			Authority:  "none",
			Reason:     "bounded_read_surface_not_connected",
		},
		Sections:           sections,
		SuggestedQuestions: questions,
		Limitations:        artifactLimitations,
		SourceRefs:         sourceRefs,
	}
}

func buildOperatorDigestSections(scope operatorDigestScope) []operatorDigestSection {
	sections := make([]operatorDigestSection, 0, len(operatorDigestSectionTemplates))
	for _, template := range operatorDigestSectionTemplates {
		limitation := operatorDigestSectionLimitation(template.ID, scope)
		sections = append(sections, operatorDigestSection{
			ID:          template.ID,
			Title:       template.Title,
			Status:      "unsupported",
			Entries:     []operatorDigestEntry{},
			Limitations: []operatorDigestLimitation{limitation},
			SourceRefs:  []string{"cli:eshu-report", "doc:operator-digest-contract"},
			Truncated:   false,
		})
	}
	return sections
}

func operatorDigestSectionLimitation(sectionID string, scope operatorDigestScope) operatorDigestLimitation {
	return operatorDigestLimitation{
		ID:     "operator_digest.v1:limitation:" + sectionID + ":" + scope.ID,
		Scope:  scope.ID,
		Reason: "unsupported_section",
		Detail: "Section " + sectionID + " requires a bounded read surface that is not connected in the offline CLI renderer.",
	}
}

func buildOperatorDigestQuestions(options operatorDigestOptions, sections []operatorDigestSection) []operatorDigestQuestion {
	limitationsBySection := make(map[string]string, len(sections))
	for _, section := range sections {
		if len(section.Limitations) > 0 {
			limitationsBySection[section.ID] = section.Limitations[0].ID
		}
	}
	questions := make([]operatorDigestQuestion, 0, len(operatorDigestQuestionTemplates))
	for _, template := range operatorDigestQuestionTemplates {
		sourceSignal := limitationsBySection[template.SectionID]
		questions = append(questions, operatorDigestQuestion{
			ID:               "operator_digest.v1:question:" + template.SectionID + ":" + options.Scope.ID,
			Question:         template.Question,
			SourceSignal:     sourceSignal,
			Why:              operatorDigestQuestionWhy(template.SectionID, sourceSignal),
			Reason:           template.Reason,
			Target:           template.Target,
			Arguments:        map[string]string{"scope": options.Scope.Label, "profile": options.Profile},
			TruthExpectation: template.TruthExpectation,
			EvidenceRefs:     []string{},
		})
	}
	return questions
}

func operatorDigestQuestionWhy(sectionID, sourceSignal string) string {
	if sourceSignal == "" {
		return "unsupported section " + sectionID + " needs a bounded read surface before this question can be answered"
	}
	return "unsupported section " + sectionID + " produced source signal " + sourceSignal
}

func renderOperatorDigestText(w io.Writer, digest operatorDigest) {
	_, _ = fmt.Fprintln(w, "Operator digest")
	_, _ = fmt.Fprintf(w, "  scope    : %s (%s)\n", digest.Scope.Label, digest.Scope.Type)
	_, _ = fmt.Fprintf(w, "  profile  : %s\n", digest.Profile)
	_, _ = fmt.Fprintf(w, "  truth    : %s freshness=%s reason=%s\n", digest.Truth.TruthClass, digest.Truth.Freshness, digest.Truth.Reason)
	_, _ = fmt.Fprintln(w, "sections:")
	for _, section := range digest.Sections {
		_, _ = fmt.Fprintf(w, "  - %s: %s\n", section.ID, section.Status)
	}
	if len(digest.SuggestedQuestions) > 0 {
		_, _ = fmt.Fprintln(w, "suggested questions:")
		for _, question := range digest.SuggestedQuestions {
			_, _ = fmt.Fprintf(w, "  - %s\n", question.Question)
			_, _ = fmt.Fprintf(w, "    id: %s\n", question.ID)
			_, _ = fmt.Fprintf(w, "    target: %s\n", question.Target)
			_, _ = fmt.Fprintf(w, "    why: %s\n", question.Why)
		}
	}
}
