// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// ask_guardrails.go — runtime guardrail helpers for POST /api/v0/ask.
//
// These functions are pure helpers called from buildAskResponse in
// ask_handler.go. They enforce citation-coverage and publish-safety invariants
// on the assembled askResponse before it is sent to the caller.

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/answerguardrail"
)

// applyAskSubstanceGuardrail withholds a circular, identity-only answer that only
// restates the question's entity and names no operational fact, even when
// citation-coverage and publish-safety pass. It runs after the derived-prose
// fallback so it evaluates whatever prose is finally published — governed
// narration or a derived deterministic summary. When it withholds prose it marks
// the answer partial and surfaces a useful next action (name the operational
// facts or the exact missing evidence) rather than publishing a tautology.
func applyAskSubstanceGuardrail(resp *askResponse, question string, primarySupported bool) {
	if resp == nil || !primarySupported {
		return
	}
	prose := strings.TrimSpace(resp.AnswerProse)
	if prose == "" {
		return
	}
	if !answerguardrail.IsCircularAnswer(question, prose) {
		return
	}
	resp.AnswerProse = ""
	resp.Artifacts = nil
	resp.Partial = true
	resp.Limitations = appendAskLimitation(resp.Limitations,
		"runtime answer guardrail withheld a circular, identity-only answer: "+string(answerguardrail.CriterionAnswerSubstance))
	resp.Limitations = appendAskLimitation(resp.Limitations,
		"name the entity's operational facts (repository, deployments, environments, API surface, dependencies) or the exact missing evidence instead of restating the entity name")
	// The evidence handles and citation ref are publish-safe and still address the
	// packet, so they are kept for follow-up rather than dropped; note explicitly
	// that only the prose was withheld so a consumer seeing partial=true with
	// citation metadata but no prose is not confused.
	if len(resp.EvidenceHandles) > 0 || strings.TrimSpace(resp.CitationRef) != "" {
		resp.Limitations = appendAskLimitation(resp.Limitations,
			"citation metadata (evidence handles and citation ref) is preserved for follow-up; only the circular prose was withheld")
	}
}

func applyAskRuntimeGuardrails(resp *askResponse, primarySupported bool) {
	if resp == nil {
		return
	}
	verdict := answerguardrail.ValidateResult(answerguardrail.Result{
		AnswerSummary:   resp.AnswerProse,
		Supported:       primarySupported,
		CitationHandles: askCitationHandleStrings(resp.EvidenceHandles),
		// Prose backed by a classified packet's truth provenance (non-empty
		// truth_class) satisfies citation coverage, mirroring the narration
		// validator's ProvenanceTruth allowance (issue #3609).
		TruthProvenance: strings.TrimSpace(resp.TruthClass) != "",
		Limitations:     resp.Limitations,
	})
	if verdict.Valid {
		return
	}
	resp.AnswerProse = ""
	resp.Artifacts = nil
	resp.Partial = true
	if verdict.HasFinding(answerguardrail.CriterionPublishSafety) {
		resp.Limitations = publishSafeAskLimitations(resp.Limitations)
		resp.EvidenceHandles = publishSafeAskEvidenceHandles(resp.EvidenceHandles)
	}
	for _, finding := range verdict.Findings {
		resp.Limitations = appendAskLimitation(resp.Limitations,
			"runtime answer guardrail blocked publishable prose: "+string(finding.Criterion))
	}
}

// applyDerivedProseFallback surfaces a supported packet's deterministic Summary
// as answer_prose when the engine produced no governed narration (issue #3550).
// It is defense-in-depth for the case where narration is unavailable or the
// narration validator rejected every sentence: without it, a fully supported
// deterministic answer would return empty prose.
//
// It runs after applyAskRuntimeGuardrails and only acts when narration produced
// no prose, the primary packet is supported, and the Summary is non-empty. The
// Summary is the packet builder's evidence-gated deterministic answer, not a
// governed narration, so the guardrail's citation-coverage rule (which targets
// governed narration prose) does not apply here. Publish safety still does: an
// unsafe Summary is never surfaced. Surfaced prose is marked derived and
// un-narrated via a limitation so callers do not mistake it for a governed
// narration.
//
// Citation coverage parity: the narration path guarantees every published
// answer carries citation coverage — inlined evidence handles, a citation_ref,
// or, for an uncitable packet, truth provenance keyed to truth_class (the #3550
// narration fix). The derived fallback matches that guarantee. It keeps any
// publish-safe EvidenceHandles or CitationRef already on resp as the coverage,
// and when the packet has neither it stamps an explicit truth-provenance
// coverage marker so the surfaced prose is never bare uncited prose.
func applyDerivedProseFallback(resp *askResponse, narrated, primarySupported bool, primarySummary string) {
	if resp == nil {
		return
	}
	if narrated || !primarySupported {
		return
	}
	if resp.AnswerProse != "" {
		return
	}
	summary := strings.TrimSpace(primarySummary)
	if summary == "" {
		return
	}
	if answerguardrail.UnsafeString(summary) {
		resp.Limitations = appendAskLimitation(resp.Limitations,
			"derived deterministic summary withheld: failed publish-safety scan")
		return
	}
	resp.AnswerProse = primarySummary
	resp.Limitations = appendAskLimitation(resp.Limitations,
		"answer_prose is the derived, un-narrated deterministic summary (no governed narration produced)")
	applyDerivedProseCoverage(resp)
}

// applyDerivedProseCoverage guarantees the derived fallback prose carries
// citation or provenance coverage, mirroring the narration path. Inlined
// publish-safe EvidenceHandles or a non-empty CitationRef already cover the
// prose, so nothing is added in those cases. When neither is present the packet
// is uncitable; the answer is still backed by its classified truth_class, so an
// explicit truth-provenance coverage marker is stamped (and the truth_class is
// echoed in it) rather than leaving the prose with no citation or provenance
// reference (issue #3550).
func applyDerivedProseCoverage(resp *askResponse) {
	if len(resp.EvidenceHandles) > 0 || strings.TrimSpace(resp.CitationRef) != "" {
		return
	}
	truthClass := strings.TrimSpace(resp.TruthClass)
	if truthClass == "" {
		truthClass = string(AnswerTruthUnsupported)
	}
	resp.Limitations = appendAskLimitation(resp.Limitations,
		"answer_prose citation coverage is the packet truth provenance (truth_class: "+truthClass+"); no citation_ref or evidence handles were resolved")
}

func askCitationHandleStrings(handles []evidenceCitationHandle) []string {
	if len(handles) == 0 {
		return nil
	}
	out := make([]string, 0, len(handles))
	for _, handle := range handles {
		parts := []string{
			handle.Kind,
			handle.RepoID,
			handle.RelativePath,
			handle.EntityID,
			handle.EvidenceFamily,
			handle.Reason,
		}
		var nonEmpty []string
		for _, part := range parts {
			if strings.TrimSpace(part) != "" {
				nonEmpty = append(nonEmpty, part)
			}
		}
		out = append(out, strings.Join(nonEmpty, ":"))
	}
	return out
}

func publishSafeAskLimitations(limitations []string) []string {
	if len(limitations) == 0 {
		return limitations
	}
	out := make([]string, 0, len(limitations))
	for _, limitation := range limitations {
		if answerguardrail.UnsafeString(limitation) {
			continue
		}
		out = append(out, limitation)
	}
	return out
}

func publishSafeAskEvidenceHandles(handles []evidenceCitationHandle) []evidenceCitationHandle {
	if len(handles) == 0 {
		return handles
	}
	out := make([]evidenceCitationHandle, 0, len(handles))
	for _, handle := range handles {
		if answerguardrail.FirstUnsafeString(askCitationHandleStrings([]evidenceCitationHandle{handle})) != "" {
			continue
		}
		out = append(out, handle)
	}
	return out
}

func appendAskLimitation(limitations []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return limitations
	}
	for _, existing := range limitations {
		if existing == value {
			return limitations
		}
	}
	return append(limitations, value)
}
