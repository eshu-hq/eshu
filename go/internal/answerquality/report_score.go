// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package answerquality

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/serviceintel"
)

// ReportVerdict is the answer-quality verdict for one service intelligence
// report. It mirrors Verdict but scores a composed report rather than captured
// prompt evidence, so the same dogfood gate covers reports.
type ReportVerdict struct {
	// Version is the report scorecard schema (ReportEvidenceVersion).
	Version string `json:"version"`
	// Subject is the report's service name.
	Subject string `json:"subject,omitempty"`
	// Pass is true when every criterion passed.
	Pass bool `json:"pass"`
	// Score is the percent of criteria that passed.
	Score int `json:"score"`
	// Criteria are the scored report criteria.
	Criteria []CriterionScore `json:"criteria"`
}

// reportCriteria is the ordered set of criteria a report is scored against. It
// is fixed so a report verdict is deterministic and comparable across runs.
var reportCriteria = []func(serviceintel.Report) CriterionScore{
	scoreReportUnsupportedClaimAvoidance,
	scoreReportCitationCoverage,
	scoreReportTruthClassPreservation,
	scoreReportLimitationVisibility,
	scoreReportTruncationSignaling,
	scoreReportNextCallExecutability,
}

// ScoreReport evaluates a composed service intelligence report against the
// report dogfood criteria. It is deterministic and offline: a report fails when
// it carries a confident unsupported claim, a citation gap, a hidden
// truncation, a missing limitation, an unexecutable next call, or an upgraded
// truth class. The verdict is suitable for CI gates and local dogfood runs.
func ScoreReport(report serviceintel.Report) ReportVerdict {
	verdict := ReportVerdict{
		Version: ReportEvidenceVersion,
		Subject: strings.TrimSpace(report.Subject.ServiceName),
		Pass:    true,
	}
	for _, score := range reportCriteria {
		criterion := score(report)
		verdict.Criteria = append(verdict.Criteria, criterion)
		if criterion.Status == CriterionFail {
			verdict.Pass = false
		}
	}
	verdict.Score = scorePercent(verdict.Criteria)
	return verdict
}

// scoreReportUnsupportedClaimAvoidance rejects a confident summary on a section
// that is unsupported, or partial with no resolved evidence. This is the core
// honesty check: an unanswerable section must not read as a fact.
func scoreReportUnsupportedClaimAvoidance(report serviceintel.Report) CriterionScore {
	for _, section := range report.Sections {
		summary := strings.TrimSpace(section.Answer.Summary)
		if summary == "" {
			continue
		}
		if !section.Answer.Supported {
			return fail(CriterionUnsupportedClaimAvoidance,
				fmt.Sprintf("section %s is unsupported but carries a confident summary", section.Kind))
		}
		if section.Answer.Partial && len(section.Answer.EvidenceHandles) == 0 {
			return fail(CriterionUnsupportedClaimAvoidance,
				fmt.Sprintf("section %s is partial with no evidence but carries a confident summary", section.Kind))
		}
	}
	return pass(CriterionUnsupportedClaimAvoidance, "no confident claim on an unsupported or evidence-less section")
}

// scoreReportCitationCoverage rejects a supported section that makes a claim
// without naming any evidence handle or citation packet.
func scoreReportCitationCoverage(report serviceintel.Report) CriterionScore {
	for _, section := range report.Sections {
		if section.Status != serviceintel.StatusSupported {
			continue
		}
		if strings.TrimSpace(section.Answer.Summary) == "" {
			continue
		}
		if len(section.Answer.EvidenceHandles) == 0 && strings.TrimSpace(section.Answer.CitationRef) == "" {
			return fail(CriterionCitationCoverage,
				fmt.Sprintf("supported section %s claims a summary with no evidence handle or citation", section.Kind))
		}
	}
	return pass(CriterionCitationCoverage, "every supported claim names evidence")
}

// scoreReportTruthClassPreservation rejects a report that upgrades or invents a
// truth class: an unsupported section claiming a non-unsupported class, a
// supported section whose class is upgraded relative to its own truth envelope,
// or a report-level class that does not match the identity anchor.
func scoreReportTruthClassPreservation(report serviceintel.Report) CriterionScore {
	for _, section := range report.Sections {
		if !section.Answer.Supported {
			if section.Answer.TruthClass != "" && section.Answer.TruthClass != query.AnswerTruthUnsupported {
				return fail(CriterionTruthClassPreservation,
					fmt.Sprintf("unsupported section %s claims truth class %q", section.Kind, section.Answer.TruthClass))
			}
			continue
		}
		// A supported section's class must match what its truth envelope maps to;
		// a content-index or fallback truth serialized as deterministic is an
		// upgrade the report must not pass.
		if section.Answer.Truth != nil {
			if want := query.ClassifyAnswerTruth(section.Answer.Truth); section.Answer.TruthClass != want {
				return fail(CriterionTruthClassPreservation,
					fmt.Sprintf("section %s truth class %q does not match its envelope (%q)", section.Kind, section.Answer.TruthClass, want))
			}
		}
	}
	if anchor, ok := identitySection(report); ok {
		if report.TruthClass != anchor.Answer.TruthClass {
			return fail(CriterionTruthClassPreservation,
				fmt.Sprintf("report truth class %q does not match identity anchor %q", report.TruthClass, anchor.Answer.TruthClass))
		}
	}
	return pass(CriterionTruthClassPreservation, "truth classes preserved from source")
}

// scoreReportLimitationVisibility rejects a partial or unsupported section that
// records neither a limitation nor an unsupported reason.
func scoreReportLimitationVisibility(report serviceintel.Report) CriterionScore {
	for _, section := range report.Sections {
		if section.Status == serviceintel.StatusSupported {
			continue
		}
		if len(section.Answer.Limitations) == 0 && len(section.Answer.UnsupportedReasons) == 0 {
			return fail(CriterionLimitationVisibility,
				fmt.Sprintf("section %s is %s but hides why", section.Kind, section.Status))
		}
	}
	return pass(CriterionLimitationVisibility, "incomplete sections explain why")
}

// scoreReportTruncationSignaling rejects hidden truncation: a section whose
// answer is truncated but is still marked supported or carries no truncation
// signal, or a report that truncates a section without marking itself partial.
func scoreReportTruncationSignaling(report serviceintel.Report) CriterionScore {
	anyTruncated := false
	for _, section := range report.Sections {
		if !section.Answer.Truncated {
			continue
		}
		anyTruncated = true
		if section.Status == serviceintel.StatusSupported {
			return fail(CriterionTruncationSignaling,
				fmt.Sprintf("section %s is truncated but marked supported", section.Kind))
		}
		if !mentionsTruncation(section.Answer.UnsupportedReasons) && !mentionsTruncation(section.Answer.Limitations) {
			return fail(CriterionTruncationSignaling,
				fmt.Sprintf("section %s is truncated but says nothing about it", section.Kind))
		}
	}
	if anyTruncated && !report.Partial {
		return fail(CriterionTruncationSignaling, "report truncates a section but is not marked partial")
	}
	return pass(CriterionTruncationSignaling, "truncation is signalled, not hidden")
}

// scoreReportNextCallExecutability rejects a recommended next call or suggested
// investigation that names no executable tool, route, or playbook.
func scoreReportNextCallExecutability(report serviceintel.Report) CriterionScore {
	for _, call := range report.NextCalls {
		if !executableCall(call) {
			return fail(CriterionNextCallExecutability, "report recommends a next call with no tool, route, or playbook")
		}
	}
	for _, section := range report.Sections {
		for _, call := range nextCallsFromAnswer(section.Answer) {
			if !executableCall(call) {
				return fail(CriterionNextCallExecutability,
					fmt.Sprintf("section %s recommends a next call with no tool, route, or playbook", section.Kind))
			}
		}
	}
	for _, investigation := range report.Investigations {
		if !executableCall(investigation.NextCall) {
			return fail(CriterionNextCallExecutability,
				fmt.Sprintf("investigation %s names no executable next call", investigation.ID))
		}
	}
	return pass(CriterionNextCallExecutability, "every next call is executable")
}

func identitySection(report serviceintel.Report) (serviceintel.ReportSection, bool) {
	for _, section := range report.Sections {
		if section.Kind == serviceintel.SectionIdentity {
			return section, true
		}
	}
	return serviceintel.ReportSection{}, false
}

func mentionsTruncation(values []string) bool {
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), "truncat") {
			return true
		}
	}
	return false
}

// executableCall reports whether a next call names a runnable target. A call
// that references a query playbook must name one that actually exists in the
// catalog (a typoed playbook id is not executable, even alongside a tool or
// route); otherwise it must name a tool or route.
func executableCall(call serviceintel.NextCall) bool {
	if playbook := strings.TrimSpace(call.Playbook); playbook != "" {
		_, ok := query.LookupPlaybook(playbook)
		return ok
	}
	return strings.TrimSpace(call.Tool) != "" || strings.TrimSpace(call.Route) != ""
}

// nextCallsFromAnswer renders an answer packet's recommended_next_calls into the
// typed NextCall shape for executability scoring.
func nextCallsFromAnswer(answer query.AnswerPacket) []serviceintel.NextCall {
	if len(answer.RecommendedNextCalls) == 0 {
		return nil
	}
	calls := make([]serviceintel.NextCall, 0, len(answer.RecommendedNextCalls))
	for _, entry := range answer.RecommendedNextCalls {
		calls = append(calls, serviceintel.NextCall{
			Tool:     mapStr(entry, "tool"),
			Route:    mapStr(entry, "route"),
			Playbook: mapStr(entry, "playbook"),
			Reason:   mapStr(entry, "reason"),
		})
	}
	return calls
}

func mapStr(entry map[string]any, key string) string {
	if value, ok := entry[key].(string); ok {
		return value
	}
	return ""
}
