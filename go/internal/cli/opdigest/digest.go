// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package opdigest

import (
	"fmt"
	"io"
	"strings"
	"unicode"
)

const (
	// Schema is the operator_digest.v1 schema marker every Digest carries.
	Schema = "operator_digest.v1"
	// DefaultProfile is the runtime profile `eshu report` resolves the
	// digest against when --profile is not set.
	DefaultProfile = "local_authoritative"
	// DefaultQuestionMax is the default --question-limit value.
	DefaultQuestionMax = 8
	// questionLimitMax is the hard contract ceiling for --question-limit.
	questionLimitMax = 25
)

var sectionTemplates = []sectionTemplate{
	{ID: "hub_services", Title: "Hub Services"},
	{ID: "cross_domain_connections", Title: "Cross-Domain Connections"},
	{ID: "ambiguity_review_queue", Title: "Ambiguity Review Queue"},
	{ID: "freshness_and_drift", Title: "Freshness And Drift"},
	{ID: "unmanaged_or_orphaned_resources", Title: "Unmanaged Or Orphaned Resources"},
}

var questionTemplates = []questionTemplate{
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

// Options is the validated, share-safe input to BuildDigest.
type Options struct {
	Scope         Scope
	Profile       string
	QuestionLimit int
}

// Digest is the operator_digest.v1 model `eshu report` renders.
type Digest struct {
	Schema             string       `json:"schema"`
	Scope              Scope        `json:"scope"`
	Profile            string       `json:"profile"`
	Truth              Truth        `json:"truth"`
	Sections           []Section    `json:"sections"`
	SuggestedQuestions []Question   `json:"suggested_questions"`
	Limitations        []Limitation `json:"limitations"`
	SourceRefs         []SourceRef  `json:"source_refs"`
}

// Scope names the share-safe operator digest target, such as
// repo:owner/name, service:name, workload:name, environment:name, or
// project:name.
type Scope struct {
	Type  string `json:"type"`
	Label string `json:"label"`
	ID    string `json:"id"`
}

// Truth carries the digest's truth-class metadata: how authoritative and
// fresh the rendered sections are.
type Truth struct {
	TruthClass string `json:"truth_class"`
	Freshness  string `json:"freshness"`
	Authority  string `json:"authority"`
	Reason     string `json:"reason"`
}

// Section is one operator_digest.v1 section, such as hub_services or
// ambiguity_review_queue.
type Section struct {
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	Status      string       `json:"status"`
	Entries     []Entry      `json:"entries"`
	Limitations []Limitation `json:"limitations"`
	SourceRefs  []string     `json:"source_refs"`
	Truncated   bool         `json:"truncated"`
}

// Entry is one row within a Section. The offline CLI renderer never
// populates entries; a connected bounded read surface would put rows here.
type Entry struct {
	ID string `json:"id"`
}

// Question is one deterministic suggested follow-up question, pointing an
// operator at a bounded route that can resolve a Section's limitation.
type Question struct {
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

// Limitation records why a Digest or Section could not produce live truth.
type Limitation struct {
	ID     string `json:"id"`
	Scope  string `json:"scope"`
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

// SourceRef names one evidence source -- a CLI command, doc, or runtime
// profile -- that a Digest or Question cites.
type SourceRef struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type sectionTemplate struct {
	ID    string
	Title string
}

type questionTemplate struct {
	SectionID        string
	Question         string
	Reason           string
	Target           string
	TruthExpectation string
}

// OptionsFromFlags validates and normalizes the raw --scope, --profile, and
// --question-limit values the `eshu report` wrapper already extracted from
// cobra flags, returning share-safe Options for BuildDigest.
//
// It does not read a *cobra.Command itself, so despite the name it is not a
// cobra-flag reader: the wrapper resolves the flags and passes plain values
// in, the same shape mcpsetup.ResolveAuthPosture uses for hosted-setup auth
// resolution. That keeps the validation and scope-parsing rules here
// unit-testable without cobra.
func OptionsFromFlags(rawScope, rawProfile string, questionLimit int) (Options, error) {
	scope, err := normalizeScope(rawScope)
	if err != nil {
		return Options{}, err
	}
	profile := strings.TrimSpace(rawProfile)
	if profile == "" {
		return Options{}, fmt.Errorf("profile is required")
	}
	if !isShareSafeToken(profile) {
		return Options{}, fmt.Errorf("profile must be share-safe")
	}
	if questionLimit < 0 || questionLimit > questionLimitMax {
		return Options{}, fmt.Errorf("question-limit must be between 0 and 25")
	}
	return Options{
		Scope:         scope,
		Profile:       profile,
		QuestionLimit: questionLimit,
	}, nil
}

func normalizeScope(raw string) (Scope, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return Scope{}, fmt.Errorf("scope is required")
	}
	if strings.Contains(value, "://") || strings.HasPrefix(value, "/") || strings.HasPrefix(value, "~") || strings.Contains(value, "\\") {
		return Scope{}, fmt.Errorf("scope must be share-safe")
	}
	scopeType := "repository"
	scopePrefix := "repo"
	label := value
	if prefix, rest, ok := strings.Cut(value, ":"); ok {
		scopePrefix = strings.TrimSpace(prefix)
		label = strings.TrimSpace(rest)
		mapped, ok := scopeTypes()[scopePrefix]
		if !ok {
			return Scope{}, fmt.Errorf("unsupported scope type %q", scopePrefix)
		}
		scopeType = mapped
	}
	if label == "" {
		return Scope{}, fmt.Errorf("scope is required")
	}
	if !isShareSafeToken(scopePrefix) || !isShareSafeLabel(label) {
		return Scope{}, fmt.Errorf("scope must be share-safe")
	}
	return Scope{
		Type:  scopeType,
		Label: label,
		ID:    scopePrefix + ":" + label,
	}, nil
}

func scopeTypes() map[string]string {
	return map[string]string{
		"repo":        "repository",
		"repository":  "repository",
		"service":     "service",
		"workload":    "workload",
		"environment": "environment",
		"project":     "project",
	}
}

func isShareSafeLabel(value string) bool {
	if len(value) > 160 || strings.HasPrefix(value, "/") || strings.HasPrefix(value, "~") || strings.Contains(value, "..") || strings.Contains(value, ":") {
		return false
	}
	return isShareSafeToken(value)
}

func isShareSafeToken(value string) bool {
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

// BuildDigest renders the deterministic operator_digest.v1 model for
// options. The offline CLI renderer never reads graph state, writes graph
// state, claims reducer work, or calls providers, so every section comes
// back status "unsupported" with a limitation naming the missing bounded
// read surface, and truth.reason is always
// "bounded_read_surface_not_connected".
func BuildDigest(options Options) Digest {
	sourceRefs := []SourceRef{
		{ID: "cli:eshu-report", Kind: "cli_command", Name: "eshu report"},
		{ID: "doc:operator-digest-contract", Kind: "documentation", Name: "operator digest contract"},
		{ID: "profile:" + options.Profile, Kind: "runtime_profile", Name: options.Profile},
	}
	artifactLimitations := []Limitation{{
		ID:     "operator_digest.v1:limitation:read_surface:" + options.Scope.ID,
		Scope:  options.Scope.ID,
		Reason: "bounded_read_surface_not_connected",
		Detail: "The CLI digest renderer validates the contract but has not connected live bounded read surfaces in this slice.",
	}}
	sections := buildSections(options.Scope)
	questions := buildQuestions(options, sections)
	if len(questions) > options.QuestionLimit {
		questions = questions[:options.QuestionLimit]
		artifactLimitations = append(artifactLimitations, Limitation{
			ID:     "operator_digest.v1:limitation:suggested_questions:" + options.Scope.ID,
			Scope:  options.Scope.ID,
			Reason: "suggested_questions_truncated",
			Detail: "Suggested questions were truncated by the requested question-limit.",
		})
	}
	return Digest{
		Schema:  Schema,
		Scope:   options.Scope,
		Profile: options.Profile,
		Truth: Truth{
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

func buildSections(scope Scope) []Section {
	sections := make([]Section, 0, len(sectionTemplates))
	for _, template := range sectionTemplates {
		limitation := sectionLimitation(template.ID, scope)
		sections = append(sections, Section{
			ID:          template.ID,
			Title:       template.Title,
			Status:      "unsupported",
			Entries:     []Entry{},
			Limitations: []Limitation{limitation},
			SourceRefs:  []string{"cli:eshu-report", "doc:operator-digest-contract"},
			Truncated:   false,
		})
	}
	return sections
}

func sectionLimitation(sectionID string, scope Scope) Limitation {
	return Limitation{
		ID:     "operator_digest.v1:limitation:" + sectionID + ":" + scope.ID,
		Scope:  scope.ID,
		Reason: "unsupported_section",
		Detail: "Section " + sectionID + " requires a bounded read surface that is not connected in the offline CLI renderer.",
	}
}

func buildQuestions(options Options, sections []Section) []Question {
	limitationsBySection := make(map[string]string, len(sections))
	for _, section := range sections {
		if len(section.Limitations) > 0 {
			limitationsBySection[section.ID] = section.Limitations[0].ID
		}
	}
	questions := make([]Question, 0, len(questionTemplates))
	for _, template := range questionTemplates {
		sourceSignal := limitationsBySection[template.SectionID]
		questions = append(questions, Question{
			ID:               "operator_digest.v1:question:" + template.SectionID + ":" + options.Scope.ID,
			Question:         template.Question,
			SourceSignal:     sourceSignal,
			Why:              questionWhy(template.SectionID, sourceSignal),
			Reason:           template.Reason,
			Target:           template.Target,
			Arguments:        map[string]string{"scope": options.Scope.Label, "profile": options.Profile},
			TruthExpectation: template.TruthExpectation,
			EvidenceRefs:     []string{},
		})
	}
	return questions
}

func questionWhy(sectionID, sourceSignal string) string {
	if sourceSignal == "" {
		return "unsupported section " + sectionID + " needs a bounded read surface before this question can be answered"
	}
	return "unsupported section " + sectionID + " produced source signal " + sourceSignal
}

// RenderText writes digest as the plain-text report `eshu report` prints by
// default (no --json).
func RenderText(w io.Writer, digest Digest) {
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
