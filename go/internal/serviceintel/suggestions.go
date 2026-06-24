// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package serviceintel

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// maxInvestigations bounds the suggested-investigation list so a report stays a
// scannable artifact regardless of how many gaps a service has.
const maxInvestigations = 12

// InvestigationBasis names the deterministic signal a suggested investigation is
// derived from. The set is closed; every suggestion ties back to one of these
// real, observable signals rather than free-form guidance.
type InvestigationBasis string

const (
	// BasisMissingEvidence marks a suggestion derived from evidence handles that
	// were requested but did not resolve.
	BasisMissingEvidence InvestigationBasis = "missing_evidence"
	// BasisStaleFreshness marks a suggestion derived from a stale or building
	// section whose freshness carries a proven cause.
	BasisStaleFreshness InvestigationBasis = "stale_freshness"
	// BasisAmbiguousTarget marks a suggestion derived from an ambiguous service
	// target: the source route could not pick a single subject.
	BasisAmbiguousTarget InvestigationBasis = "ambiguous_target"
	// BasisUnsupportedLane marks a suggestion derived from a section whose
	// evidence lane is unavailable.
	BasisUnsupportedLane InvestigationBasis = "unsupported_lane"
	// BasisHighImpactRelationship marks a suggestion derived from a supported
	// section the caller flagged as carrying a high-impact relationship.
	BasisHighImpactRelationship InvestigationBasis = "high_impact_relationship"
)

// SuggestedInvestigation is one deterministic, executable follow-up an operator
// should run next. Every suggestion ties a concrete signal (Basis,
// EvidenceBasis) to a bounded NextCall that names a real tool, route, or query
// playbook, and declares the truth class the operator should expect from it.
type SuggestedInvestigation struct {
	// ID is the stable identifier, deterministic for a given section, basis, and
	// target. It is used to de-duplicate suggestions.
	ID string `json:"id"`
	// Section is the report section the suggestion was derived from.
	Section SectionKind `json:"section"`
	// Basis names the signal the suggestion was derived from.
	Basis InvestigationBasis `json:"basis"`
	// Reason explains, in one human sentence, why this investigation is suggested.
	Reason string `json:"reason"`
	// EvidenceBasis lists the concrete signal references (missing handle keys, a
	// freshness cause, an ambiguity message) that ground the suggestion.
	EvidenceBasis []string `json:"evidence_basis,omitempty"`
	// NextCall is the bounded, executable follow-up call.
	NextCall NextCall `json:"next_call"`
	// ExpectedTruthClass is the truth class the operator should expect the
	// NextCall to yield. It is sourced from the section truth or the linked
	// playbook, never invented.
	ExpectedTruthClass query.AnswerTruthClass `json:"expected_truth_class"`
}

// suggestInvestigations derives the bounded, de-duplicated, ordered guided
// investigations for a section from its supplied input and composed answer. It
// appends to the running report so callers compose investigations in catalog
// order across sections.
func suggestInvestigations(report *Report, spec sectionSpec, input SectionInput, section ReportSection) {
	for _, candidate := range sectionInvestigations(spec, input, section, report.Subject) {
		if len(report.Investigations) >= maxInvestigations {
			return
		}
		report.Investigations = appendUniqueInvestigation(report.Investigations, candidate)
	}
}

// sectionInvestigations returns the candidate investigations for one section in
// a fixed priority order: ambiguous target, unsupported lane, missing evidence,
// stale freshness, then flagged high-impact relationship. A section with no
// supporting signal returns none.
func sectionInvestigations(spec sectionSpec, input SectionInput, section ReportSection, subject ReportSubject) []SuggestedInvestigation {
	var out []SuggestedInvestigation

	ambiguous := isAmbiguousInput(input)
	if ambiguous {
		out = append(out, ambiguousTargetInvestigation(spec, input, section, subject))
	}

	// An ambiguous section gets a disambiguation suggestion, not an
	// unsupported-lane one: the lane is not down, the target is unresolved.
	if section.Status == StatusUnsupported && !ambiguous {
		out = append(out, unsupportedLaneInvestigation(spec, section, subject))
	}

	if len(section.Answer.MissingEvidence) > 0 {
		out = append(out, missingEvidenceInvestigation(spec, section))
	}

	if cause, check, ok := staleFreshnessSignal(section); ok {
		out = append(out, staleFreshnessInvestigation(spec, section, cause, check))
	}

	// A high-impact drilldown is only executable with a concrete resolved id, so
	// emit it only when the section's evidence carries an entity id to follow.
	if resolvedID := firstEntityID(input.Evidence); input.HighImpact && section.Status == StatusSupported && resolvedID != "" {
		out = append(out, highImpactInvestigation(spec, input, section, resolvedID))
	}

	return out
}

func ambiguousTargetInvestigation(spec sectionSpec, input SectionInput, section ReportSection, subject ReportSubject) SuggestedInvestigation {
	call := NextCall{
		Tool:   "resolve_entity",
		Route:  "/api/v0/entities/resolve",
		Reason: "resolve the ambiguous service target to a single entity before re-running this section",
	}
	// resolve_entity rejects an empty name, so pass the subject name as the
	// bounded resolver input; without it the suggestion would dispatch a 400.
	if name := strings.TrimSpace(subject.ServiceName); name != "" {
		call.Arguments = map[string]any{"name": name}
	}
	return SuggestedInvestigation{
		ID:                 investigationID(spec.Kind, BasisAmbiguousTarget, "resolve_entity"),
		Section:            spec.Kind,
		Basis:              BasisAmbiguousTarget,
		Reason:             fmt.Sprintf("The %s target is ambiguous; resolve it to a specific entity rather than guessing a winner.", strings.ToLower(spec.Title)),
		EvidenceBasis:      ambiguityEvidence(input.Err),
		NextCall:           call,
		ExpectedTruthClass: expectedTruthClass(section, call),
	}
}

func unsupportedLaneInvestigation(spec sectionSpec, section ReportSection, subject ReportSubject) SuggestedInvestigation {
	// Use the same resolved fallback the section surfaces, so the investigation
	// recommends an executable call: tools that cannot run on a service name
	// alone (compare_environments, get_incident_context) are reduced to their
	// orchestrating playbook rather than mis-called.
	call := fallbackNextCall(spec, subject)
	return SuggestedInvestigation{
		ID:                 investigationID(spec.Kind, BasisUnsupportedLane, fallbackTarget(call)),
		Section:            spec.Kind,
		Basis:              BasisUnsupportedLane,
		Reason:             fmt.Sprintf("The %s evidence lane is unavailable; gather it with the recommended call.", strings.ToLower(spec.Title)),
		EvidenceBasis:      dedupeStrings(section.Answer.UnsupportedReasons),
		NextCall:           call,
		ExpectedTruthClass: expectedTruthClass(section, call),
	}
}

func missingEvidenceInvestigation(spec sectionSpec, section ReportSection) SuggestedInvestigation {
	call := NextCall{
		Tool:     "build_evidence_citation_packet",
		Route:    "/api/v0/evidence/citations",
		Playbook: "service_story_citation",
		Reason:   "hydrate the unresolved evidence handles into a citation packet",
	}
	// The citation packet request rejects an empty handles list, so carry the
	// unresolved handles as the bounded call input.
	if handles := citationHandleArgs(section.Answer.MissingEvidence); len(handles) > 0 {
		call.Arguments = map[string]any{"handles": handles}
	}
	return SuggestedInvestigation{
		ID:                 investigationID(spec.Kind, BasisMissingEvidence, "build_evidence_citation_packet"),
		Section:            spec.Kind,
		Basis:              BasisMissingEvidence,
		Reason:             fmt.Sprintf("%d evidence handle(s) for the %s did not resolve; hydrate them before trusting the section as complete.", len(section.Answer.MissingEvidence), strings.ToLower(spec.Title)),
		EvidenceBasis:      handleKeys(section.Answer.MissingEvidence),
		NextCall:           call,
		ExpectedTruthClass: expectedTruthClass(section, call),
	}
}

func staleFreshnessInvestigation(spec sectionSpec, section ReportSection, cause query.FreshnessCause, check *query.FreshnessNextCheck) SuggestedInvestigation {
	call := freshnessNextCall(check)
	return SuggestedInvestigation{
		ID:                 investigationID(spec.Kind, BasisStaleFreshness, string(cause)),
		Section:            spec.Kind,
		Basis:              BasisStaleFreshness,
		Reason:             fmt.Sprintf("The %s lags (cause: %s); drill into the bounded freshness check to learn when it catches up.", strings.ToLower(spec.Title), cause),
		EvidenceBasis:      []string{string(cause)},
		NextCall:           call,
		ExpectedTruthClass: expectedTruthClass(section, call),
	}
}

func highImpactInvestigation(spec sectionSpec, input SectionInput, section ReportSection, resolvedID string) SuggestedInvestigation {
	call := NextCall{
		Tool:   "get_relationship_evidence",
		Route:  "/api/v0/evidence/relationships/{resolved_id}",
		Reason: "inspect the high-impact relationship evidence behind this section",
		// get_relationship_evidence builds its path from resolved_id and rejects
		// an empty value, so pass the concrete entity id from the section evidence.
		Arguments: map[string]any{"resolved_id": resolvedID},
	}
	return SuggestedInvestigation{
		ID:                 investigationID(spec.Kind, BasisHighImpactRelationship, "get_relationship_evidence"),
		Section:            spec.Kind,
		Basis:              BasisHighImpactRelationship,
		Reason:             fmt.Sprintf("The %s carries a high-impact relationship; verify its evidence before relying on it for change decisions.", strings.ToLower(spec.Title)),
		EvidenceBasis:      handleKeys(input.Evidence),
		NextCall:           call,
		ExpectedTruthClass: expectedTruthClass(section, call),
	}
}

// staleFreshnessSignal reports whether a section is stale or building with a
// proven cause and a bounded, executable next check. It returns the cause and
// the next check so callers never re-dereference the truth envelope.
func staleFreshnessSignal(section ReportSection) (query.FreshnessCause, *query.FreshnessNextCheck, bool) {
	truth := section.Answer.Truth
	if truth == nil {
		return "", nil, false
	}
	switch truth.Freshness.State {
	case query.FreshnessStale, query.FreshnessBuilding:
	default:
		return "", nil, false
	}
	if !query.ValidFreshnessCause(truth.Freshness.Cause) {
		return "", nil, false
	}
	// A suggestion must be executable: require a bounded next check that names a
	// tool or route, never an empty drilldown.
	check := truth.Freshness.NextCheck
	if check == nil || (strings.TrimSpace(check.Tool) == "" && strings.TrimSpace(check.Route) == "") {
		return "", nil, false
	}
	return truth.Freshness.Cause, check, true
}

// freshnessNextCall renders a bounded freshness next check as a typed NextCall.
// The check must be non-nil; callers obtain it from staleFreshnessSignal. The
// check's scoped Params (repository, generation, source, or status filters) are
// preserved as call arguments so the drilldown stays narrowly targeted, matching
// what AnswerPacket carries in recommended_next_calls.
func freshnessNextCall(check *query.FreshnessNextCheck) NextCall {
	call := NextCall{
		Tool:   strings.TrimSpace(check.Tool),
		Route:  strings.TrimSpace(check.Route),
		Reason: strings.TrimSpace(check.Reason),
	}
	if len(check.Params) > 0 {
		args := make(map[string]any, len(check.Params))
		for key, value := range check.Params {
			args[key] = value
		}
		call.Arguments = args
	}
	return call
}

// firstEntityID returns the first non-empty entity id from a set of evidence
// handles, used as the concrete resolved id for a relationship drilldown.
func firstEntityID(handles []query.EvidenceCitationHandle) string {
	for _, handle := range handles {
		if id := strings.TrimSpace(handle.EntityID); id != "" {
			return id
		}
	}
	return ""
}

// citationHandleArgs renders unresolved evidence handles into the bounded
// handle-input shape the evidence-citation request accepts, so the hydration
// call carries the very handles the missing-evidence basis surfaced.
func citationHandleArgs(handles []query.EvidenceCitationHandle) []map[string]any {
	if len(handles) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(handles))
	for _, handle := range handles {
		entry := map[string]any{}
		if handle.Kind != "" {
			entry["kind"] = handle.Kind
		}
		if handle.RepoID != "" {
			entry["repo_id"] = handle.RepoID
		}
		if handle.RelativePath != "" {
			entry["relative_path"] = handle.RelativePath
		}
		if handle.EntityID != "" {
			entry["entity_id"] = handle.EntityID
		}
		if len(entry) > 0 {
			out = append(out, entry)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// expectedTruthClass sources the truth class an operator should expect from a
// suggestion's next call. A section that already carries truth keeps that class
// (completing or refreshing it yields the same axis); otherwise a linked
// playbook's terminal expected truth is used; otherwise authoritative resolution
// is assumed. The class is never invented beyond these grounded sources.
func expectedTruthClass(section ReportSection, call NextCall) query.AnswerTruthClass {
	if section.Answer.Truth != nil && section.Answer.TruthClass != "" {
		return section.Answer.TruthClass
	}
	if call.Playbook != "" {
		if class, ok := playbookTerminalTruth(call.Playbook); ok {
			return class
		}
	}
	return query.AnswerTruthDeterministic
}

// playbookTerminalTruth returns the terminal step's expected truth class for a
// playbook, the truth the workflow is declared to reach.
func playbookTerminalTruth(id string) (query.AnswerTruthClass, bool) {
	playbook, ok := query.LookupPlaybook(id)
	if !ok || len(playbook.Steps) == 0 {
		return "", false
	}
	class := playbook.Steps[len(playbook.Steps)-1].ExpectedTruth
	if class == "" {
		return "", false
	}
	return class, true
}

// appendUniqueInvestigation appends a suggestion when no existing suggestion
// shares its ID, keeping the list de-duplicated and order-stable.
func appendUniqueInvestigation(list []SuggestedInvestigation, candidate SuggestedInvestigation) []SuggestedInvestigation {
	for _, existing := range list {
		if existing.ID == candidate.ID {
			return list
		}
	}
	return append(list, candidate)
}

// isAmbiguousInput reports whether a section's source route failed because the
// service target was ambiguous.
func isAmbiguousInput(input SectionInput) bool {
	return input.Err != nil && input.Err.Code == query.ErrorCodeAmbiguous
}

func investigationID(kind SectionKind, basis InvestigationBasis, target string) string {
	return fmt.Sprintf("%s:%s:%s", kind, basis, target)
}

func ambiguityEvidence(err *query.ErrorEnvelope) []string {
	if err == nil {
		return nil
	}
	message := strings.TrimSpace(err.Message)
	if message == "" {
		return []string{string(err.Code)}
	}
	return []string{message}
}

func fallbackTarget(call NextCall) string {
	switch {
	case call.Tool != "":
		return call.Tool
	case call.Route != "":
		return call.Route
	case call.Playbook != "":
		return call.Playbook
	default:
		return "fallback"
	}
}

// handleKeys renders evidence handles as stable, low-cardinality keys for the
// evidence basis, preferring entity id then repo-qualified path.
func handleKeys(handles []query.EvidenceCitationHandle) []string {
	if len(handles) == 0 {
		return nil
	}
	keys := make([]string, 0, len(handles))
	for _, handle := range handles {
		keys = appendUniqueString(keys, handleKey(handle))
	}
	if len(keys) == 0 {
		return nil
	}
	return keys
}

func handleKey(handle query.EvidenceCitationHandle) string {
	if id := strings.TrimSpace(handle.EntityID); id != "" {
		return id
	}
	repo := strings.TrimSpace(handle.RepoID)
	path := strings.TrimSpace(handle.RelativePath)
	switch {
	case repo != "" && path != "":
		return repo + ":" + path
	case path != "":
		return path
	case repo != "":
		return repo
	default:
		return strings.TrimSpace(handle.Kind)
	}
}

func dedupeStrings(values []string) []string {
	var out []string
	for _, value := range values {
		out = appendUniqueString(out, value)
	}
	return out
}
