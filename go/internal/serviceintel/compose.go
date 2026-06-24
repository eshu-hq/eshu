// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package serviceintel

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// Compose arranges per-section answer evidence into a deterministic service
// intelligence report. It is a pure function: the same ReportInput always
// yields a byte-identical Report. Compose introduces no new truth source — each
// section's truth is the source route's truth, classified by the canonical
// AnswerPacket builder, and the report-level truth is anchored on the identity
// section. Sections without resolved evidence stay visible as partial or
// unsupported with explicit limitations and bounded next calls.
func Compose(in ReportInput) Report {
	supplied := make(map[SectionKind]SectionInput, len(in.Sections))
	for _, section := range in.Sections {
		if _, known := specForKind(section.Kind); !known {
			continue
		}
		if _, seen := supplied[section.Kind]; seen {
			continue // first input for a kind wins; keep composition deterministic
		}
		supplied[section.Kind] = section
	}

	report := Report{Schema: ReportSchema, Subject: in.Subject}
	report.Sections = make([]ReportSection, 0, len(sectionCatalog))
	for _, spec := range sectionCatalog {
		input := supplied[spec.Kind]
		section := composeSection(spec, input, in.Subject)
		report.Sections = append(report.Sections, section)
		accumulateReport(&report, spec.Kind, section)
		suggestInvestigations(&report, spec, input, section)
	}
	return report
}

// composeSection builds one report section from its static spec and the
// caller-supplied evidence. It delegates truth classification and the honesty
// contract to query.NewAnswerPacket, then derives the section status from the
// resulting packet.
func composeSection(spec sectionSpec, input SectionInput, subject ReportSubject) ReportSection {
	needsFallback := sectionNeedsFallback(input)

	nextCalls := append([]NextCall(nil), input.NextCalls...)
	if needsFallback {
		nextCalls = appendUniqueNextCall(nextCalls, fallbackNextCall(spec, subject))
	}

	limitations := append([]string(nil), input.Limitations...)
	if needsFallback {
		limitations = appendUniqueString(limitations, fallbackLimitation(spec, subject))
	}

	packet := query.NewAnswerPacket(query.AnswerPacketInput{
		PromptFamily:         spec.PromptFamily,
		Question:             sectionQuestion(spec, subject),
		PrimaryTool:          spec.Tool,
		PrimaryRoute:         spec.Route,
		Summary:              input.Summary,
		Limitations:          limitations,
		Truncated:            input.Truncated,
		NoEvidence:           input.NoEvidence,
		EvidenceHandles:      input.Evidence,
		MissingEvidence:      input.MissingEvidence,
		RecommendedNextCalls: nextCallsToMaps(nextCalls),
		Envelope:             sectionEnvelope(spec, input, subject),
	})

	return ReportSection{
		Kind:   spec.Kind,
		Title:  spec.Title,
		Status: statusFromPacket(packet),
		Answer: packet,
	}
}

// sectionNeedsFallback reports whether a section lacks resolved, fresh evidence
// and therefore must surface a fallback next call and limitation.
func sectionNeedsFallback(input SectionInput) bool {
	if input.Err != nil || input.Truth == nil {
		return true
	}
	if input.Truncated {
		return true
	}
	if len(input.MissingEvidence) > 0 {
		return true
	}
	if input.NoEvidence && len(input.Evidence) == 0 {
		return true
	}
	// Mirror AnswerPacket.markPartial exactly: stale and building mark a section
	// partial, so they need a fallback. Other freshness states (including
	// unavailable, which arrives with no evidence and is already covered above)
	// must not be reclassified by the composer.
	switch input.Truth.Freshness.State {
	case query.FreshnessStale, query.FreshnessBuilding:
		return true
	}
	return false
}

// sectionEnvelope builds the ResponseEnvelope handed to the AnswerPacket
// builder. A source error or absent truth yields an error envelope (unsupported
// section); otherwise the source truth is preserved verbatim.
func sectionEnvelope(spec sectionSpec, input SectionInput, subject ReportSubject) *query.ResponseEnvelope {
	if input.Err != nil {
		return &query.ResponseEnvelope{Error: input.Err}
	}
	if input.Truth == nil {
		return &query.ResponseEnvelope{Error: &query.ErrorEnvelope{
			Code:    query.ErrorCodeNotFound,
			Message: fmt.Sprintf("no %s evidence available for service %s", strings.ToLower(spec.Title), subjectName(subject)),
		}}
	}
	return &query.ResponseEnvelope{Truth: input.Truth}
}

// statusFromPacket derives a section status from an embedded answer packet,
// never reclassifying the source truth.
func statusFromPacket(packet query.AnswerPacket) SectionStatus {
	switch {
	case !packet.Supported:
		return StatusUnsupported
	case packet.Partial:
		return StatusPartial
	default:
		return StatusSupported
	}
}

// accumulateReport folds one composed section into the report aggregates: the
// identity section anchors report-level truth and support; every section
// contributes to the partial flag, limitations, and next calls.
func accumulateReport(report *Report, kind SectionKind, section ReportSection) {
	if kind == SectionIdentity {
		report.Supported = section.Answer.Supported
		report.TruthClass = section.Answer.TruthClass
		report.Truth = section.Answer.Truth
	}
	if section.Status != StatusSupported {
		report.Partial = true
	}
	for _, limitation := range section.Answer.Limitations {
		report.Limitations = appendUniqueString(report.Limitations, limitation)
	}
	for _, call := range nextCallsFromMaps(section.Answer.RecommendedNextCalls) {
		report.NextCalls = appendUniqueNextCall(report.NextCalls, call)
	}
}

func sectionQuestion(spec sectionSpec, subject ReportSubject) string {
	return fmt.Sprintf("What is the %s for service %s?", strings.ToLower(spec.Title), subjectName(subject))
}

func fallbackLimitation(spec sectionSpec, subject ReportSubject) string {
	return fmt.Sprintf("%s is not fully resolved for service %s; run the recommended next call to gather it", spec.Title, subjectName(subject))
}

func fallbackNextCall(spec sectionSpec, subject ReportSubject) NextCall {
	call := spec.Fallback
	if args := subjectArguments(subject); len(args) > 0 {
		call.Arguments = args
	}
	switch call.Tool {
	case "compare_environments", "get_incident_context":
		call.Tool = ""
		call.Route = ""
	}
	return call
}

func subjectArguments(subject ReportSubject) map[string]any {
	serviceName := strings.TrimSpace(subject.ServiceName)
	if serviceName == "" {
		return nil
	}
	return map[string]any{"service_name": serviceName}
}

func subjectName(subject ReportSubject) string {
	name := strings.TrimSpace(subject.ServiceName)
	if name == "" {
		return "(unknown)"
	}
	return name
}
