// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package answerquality

import (
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/serviceintel"
)

// ReportFixture is one named service intelligence report and its expected
// scorecard outcome. The corpus is share-safe synthetic data so it can ship in
// the repo and back a CI or local dogfood gate.
type ReportFixture struct {
	// Name identifies the fixture (happy path or a named failure mode).
	Name string
	// Report is the composed or deliberately broken report under test.
	Report serviceintel.Report
	// ExpectPass is the expected ScoreReport pass result.
	ExpectPass bool
	// ExpectFailing lists the criteria expected to fail. It is empty for a
	// passing fixture and exact for a failing one.
	ExpectFailing []CriterionName
}

// ReportCorpus returns the canonical report fixture corpus: one honest happy
// path, one honest partial report, and one fixture per failure mode the
// scorecard must reject. Each failure fixture mutates a freshly composed honest
// report minimally, so it isolates a single criterion.
func ReportCorpus() []ReportFixture {
	fixtures := []ReportFixture{
		{Name: "happy_complete", Report: honestCompleteReport(), ExpectPass: true},
		{Name: "honest_partial_stale", Report: honestStaleReport(), ExpectPass: true},
	}

	confidentUnsupported := honestCompleteReport()
	mutateSection(&confidentUnsupported, serviceintel.SectionSupplyChain, func(s *serviceintel.ReportSection) {
		s.Status = serviceintel.StatusUnsupported
		s.Answer.Supported = false
		s.Answer.TruthClass = query.AnswerTruthUnsupported
		s.Answer.UnsupportedReasons = []string{"supply-chain lane unavailable"}
		s.Answer.Summary = "supply chain is clean" // dishonest: confident on an unsupported section
	})
	fixtures = append(fixtures, ReportFixture{
		Name:          "confident_unsupported_claim",
		Report:        confidentUnsupported,
		ExpectFailing: []CriterionName{CriterionUnsupportedClaimAvoidance},
	})

	citationGap := honestCompleteReport()
	mutateSection(&citationGap, serviceintel.SectionSupplyChain, func(s *serviceintel.ReportSection) {
		s.Answer.EvidenceHandles = nil
		s.Answer.CitationRef = "" // claims a summary with nothing behind it
	})
	fixtures = append(fixtures, ReportFixture{
		Name:          "citation_gap",
		Report:        citationGap,
		ExpectFailing: []CriterionName{CriterionCitationCoverage},
	})

	missingLimitations := honestCompleteReport()
	mutateSection(&missingLimitations, serviceintel.SectionSupplyChain, func(s *serviceintel.ReportSection) {
		s.Status = serviceintel.StatusUnsupported
		s.Answer.Supported = false
		s.Answer.TruthClass = query.AnswerTruthUnsupported
		s.Answer.Summary = ""
		s.Answer.Limitations = nil
		s.Answer.UnsupportedReasons = nil // hides why it is incomplete
	})
	fixtures = append(fixtures, ReportFixture{
		Name:          "missing_limitations",
		Report:        missingLimitations,
		ExpectFailing: []CriterionName{CriterionLimitationVisibility},
	})

	hiddenTruncation := honestCompleteReport()
	mutateSection(&hiddenTruncation, serviceintel.SectionSupplyChain, func(s *serviceintel.ReportSection) {
		s.Answer.Truncated = true // truncated but still marked supported, says nothing
	})
	fixtures = append(fixtures, ReportFixture{
		Name:          "hidden_truncation",
		Report:        hiddenTruncation,
		ExpectFailing: []CriterionName{CriterionTruncationSignaling},
	})

	truthUpgrade := honestCompleteReport()
	truthUpgrade.TruthClass = query.AnswerTruthCodeHint // does not match the deterministic identity anchor
	fixtures = append(fixtures, ReportFixture{
		Name:          "truth_class_upgrade",
		Report:        truthUpgrade,
		ExpectFailing: []CriterionName{CriterionTruthClassPreservation},
	})

	emptyNextCall := honestCompleteReport()
	emptyNextCall.Investigations = append(emptyNextCall.Investigations, serviceintel.SuggestedInvestigation{
		ID:      "broken:empty_next_call",
		Section: serviceintel.SectionSupplyChain,
		Basis:   serviceintel.BasisUnsupportedLane,
		Reason:  "this suggestion names no executable call",
	})
	fixtures = append(fixtures, ReportFixture{
		Name:          "unexecutable_next_call",
		Report:        emptyNextCall,
		ExpectFailing: []CriterionName{CriterionNextCallExecutability},
	})

	silentPartialTruncation := honestCompleteReport()
	silentPartialTruncation.Partial = true
	mutateSection(&silentPartialTruncation, serviceintel.SectionSupplyChain, func(s *serviceintel.ReportSection) {
		s.Status = serviceintel.StatusPartial
		s.Answer.Partial = true
		s.Answer.Truncated = true
		s.Answer.Summary = ""
		// A non-truncation reason keeps limitation_visibility satisfied while the
		// truncation itself is never disclosed.
		s.Answer.UnsupportedReasons = []string{"underlying data is stale"}
		s.Answer.Limitations = nil
	})
	fixtures = append(fixtures, ReportFixture{
		Name:          "silent_partial_truncation",
		Report:        silentPartialTruncation,
		ExpectFailing: []CriterionName{CriterionTruncationSignaling},
	})

	partialClaimNoEvidence := honestCompleteReport()
	partialClaimNoEvidence.Partial = true
	mutateSection(&partialClaimNoEvidence, serviceintel.SectionSupplyChain, func(s *serviceintel.ReportSection) {
		s.Status = serviceintel.StatusPartial
		s.Answer.Partial = true
		s.Answer.EvidenceHandles = nil // partial with a confident summary but nothing behind it
		s.Answer.UnsupportedReasons = []string{"underlying data is stale"}
	})
	fixtures = append(fixtures, ReportFixture{
		Name:          "partial_claim_without_evidence",
		Report:        partialClaimNoEvidence,
		ExpectFailing: []CriterionName{CriterionUnsupportedClaimAvoidance},
	})

	upgradedSupportedTruth := honestCompleteReport()
	mutateSection(&upgradedSupportedTruth, serviceintel.SectionSupplyChain, func(s *serviceintel.ReportSection) {
		// Content-index truth serialized as deterministic: an upgrade vs the
		// section's own envelope that the criterion must catch.
		s.Answer.Truth = &query.TruthEnvelope{Level: query.TruthLevelDerived, Basis: query.TruthBasisContentIndex}
		s.Answer.TruthClass = query.AnswerTruthDeterministic
	})
	fixtures = append(fixtures, ReportFixture{
		Name:          "supported_section_truth_upgrade",
		Report:        upgradedSupportedTruth,
		ExpectFailing: []CriterionName{CriterionTruthClassPreservation},
	})

	typoedPlaybook := honestCompleteReport()
	typoedPlaybook.Investigations = append(typoedPlaybook.Investigations, serviceintel.SuggestedInvestigation{
		ID:       "broken:typoed_playbook",
		Section:  serviceintel.SectionSupplyChain,
		Basis:    serviceintel.BasisUnsupportedLane,
		Reason:   "this suggestion names a playbook that does not exist",
		NextCall: serviceintel.NextCall{Playbook: "service_story_citaton"}, // typo
	})
	fixtures = append(fixtures, ReportFixture{
		Name:          "nonexistent_playbook_next_call",
		Report:        typoedPlaybook,
		ExpectFailing: []CriterionName{CriterionNextCallExecutability},
	})

	return fixtures
}

// honestCompleteReport composes a fully supported report through the real
// composer, so it is honest by construction and passes every criterion.
func honestCompleteReport() serviceintel.Report {
	return serviceintel.Compose(serviceintel.ReportInput{
		Subject:  serviceintel.ReportSubject{ServiceName: "checkout", ServiceID: "svc:checkout"},
		Sections: completeSectionInputs(),
	})
}

// honestStaleReport composes a report whose deployment section is stale with a
// proven cause: honest and incomplete, so it must still pass every criterion.
func honestStaleReport() serviceintel.Report {
	sections := completeSectionInputs()
	for i := range sections {
		if sections[i].Kind == serviceintel.SectionDeploymentConfig {
			sections[i].Truth = &query.TruthEnvelope{
				Level: query.TruthLevelExact,
				Basis: query.TruthBasisAuthoritativeGraph,
				Freshness: query.TruthFreshness{
					State: query.FreshnessStale,
					Cause: query.FreshnessCauseReducerBacklog,
					NextCheck: &query.FreshnessNextCheck{
						Tool:   "get_reducer_status",
						Reason: "check reducer backlog drain",
					},
				},
			}
		}
	}
	return serviceintel.Compose(serviceintel.ReportInput{
		Subject:  serviceintel.ReportSubject{ServiceName: "checkout", ServiceID: "svc:checkout"},
		Sections: sections,
	})
}

// completeSectionInputs returns a supported, evidence-backed input for every
// report section using share-safe synthetic data.
func completeSectionInputs() []serviceintel.SectionInput {
	kinds := []serviceintel.SectionKind{
		serviceintel.SectionIdentity,
		serviceintel.SectionCodeToRuntime,
		serviceintel.SectionDeploymentConfig,
		serviceintel.SectionSupplyChain,
		serviceintel.SectionIncidentsSupport,
	}
	inputs := make([]serviceintel.SectionInput, 0, len(kinds))
	for _, kind := range kinds {
		inputs = append(inputs, serviceintel.SectionInput{
			Kind:    kind,
			Summary: "evidence-backed " + string(kind),
			Truth: &query.TruthEnvelope{
				Level:     query.TruthLevelExact,
				Basis:     query.TruthBasisAuthoritativeGraph,
				Freshness: query.TruthFreshness{State: query.FreshnessFresh},
			},
			Evidence: []query.EvidenceCitationHandle{
				{Kind: "source", RepoID: "repo:checkout", RelativePath: string(kind) + ".go"},
			},
		})
	}
	return inputs
}

// mutateSection applies a mutation to the named section of a report in place.
func mutateSection(report *serviceintel.Report, kind serviceintel.SectionKind, mutate func(*serviceintel.ReportSection)) {
	for i := range report.Sections {
		if report.Sections[i].Kind == kind {
			mutate(&report.Sections[i])
			return
		}
	}
}
