// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// decodeCodeTaintEvidenceInput decodes one code_taint_evidence envelope
// through the contracts seam (decodeCodeTaintEvidence) and projects it into
// the reducer's CodeTaintEvidenceInput row, RETURNING the decode error rather
// than swallowing it. A missing required function_uid (or any other
// malformed/unsupported-major payload) surfaces as a classified
// *factDecodeError so the caller (ExtractCodeTaintEvidenceRowsWithQuarantine)
// routes it through partitionDecodeFailures and dead-letters it as a visible
// input_invalid quarantine — the accuracy guarantee epic #4566 §1 exists to
// enforce, not the pre-Contract-System silent-empty-string behavior.
//
// Every string field is TrimSpace'd, mirroring the pre-Contract-System
// payloadString helper's universal trim (go/internal/storage/postgres
// secrets_iam_trust_chain_evidence_loader.go): the typed decode seam itself
// does not trim, so a padded function_uid would otherwise flow through
// untrimmed into the graph node attachment key.
func decodeCodeTaintEvidenceInput(envelope facts.Envelope) (CodeTaintEvidenceInput, error) {
	evidence, err := decodeCodeTaintEvidence(envelope)
	if err != nil {
		return CodeTaintEvidenceInput{}, err
	}
	return CodeTaintEvidenceInput{
		FunctionUID:  strings.TrimSpace(evidence.FunctionUID),
		FunctionName: derefStringTrimmed(evidence.FunctionName),
		RelativePath: derefStringTrimmed(evidence.RelativePath),
		Language:     derefStringTrimmed(evidence.Language),
		Kind:         derefStringTrimmed(evidence.Kind),
		SinkKind:     derefStringTrimmed(evidence.SinkKind),
		SourceKind:   derefStringTrimmed(evidence.SourceKind),
		Binding:      derefStringTrimmed(evidence.Binding),
		SourceLine:   derefInt(evidence.SourceLine),
		SinkLine:     derefInt(evidence.SinkLine),
		Confidence:   derefFloat64(evidence.Confidence),
		ClassContext: derefStringTrimmed(evidence.ClassContext),
		SinkLabel:    derefStringTrimmed(evidence.SinkLabel),
		SourceLabel:  derefStringTrimmed(evidence.SourceLabel),
		GuardReason:  derefStringTrimmed(evidence.GuardReason),
	}, nil
}

// decodeCodeInterprocEvidenceInput decodes one code_interproc_evidence
// envelope through the contracts seam (decodeCodeInterprocEvidence) and
// projects it into the reducer's CodeInterprocEvidenceInput row, RETURNING the
// decode error so the caller (ExtractCodeInterprocEvidenceRowsWithQuarantine)
// dead-letters a missing required source_function_uid/sink_function_uid as an
// input_invalid quarantine. Every string field is TrimSpace'd, same rationale
// as decodeCodeTaintEvidenceInput.
func decodeCodeInterprocEvidenceInput(envelope facts.Envelope) (CodeInterprocEvidenceInput, error) {
	evidence, err := decodeCodeInterprocEvidence(envelope)
	if err != nil {
		return CodeInterprocEvidenceInput{}, err
	}
	return CodeInterprocEvidenceInput{
		SourceFunctionUID:  strings.TrimSpace(evidence.SourceFunctionUID),
		SinkFunctionUID:    strings.TrimSpace(evidence.SinkFunctionUID),
		RelativePath:       derefStringTrimmed(evidence.RelativePath),
		SourceFunctionName: derefStringTrimmed(evidence.SourceFunctionName),
		SinkFunctionName:   derefStringTrimmed(evidence.SinkFunctionName),
		Language:           derefStringTrimmed(evidence.Language),
		SinkKind:           derefStringTrimmed(evidence.SinkKind),
		SourceKind:         derefStringTrimmed(evidence.SourceKind),
		Confidence:         derefFloat64(evidence.Confidence),
		Cloud:              derefBool(evidence.Cloud),
		WhyTrail:           evidence.WhyTrail,
		WhyTrailTruncated:  derefBool(evidence.WhyTrailTruncated),
	}, nil
}

// ExtractCodeTaintEvidenceRowsWithQuarantine decodes each code_taint_evidence
// envelope through the typed contracts seam and returns the projected graph
// rows plus the per-fact input_invalid quarantines (Contract System v1
// Wave 4f S2, issue #4754). This is the production decode+quarantine path the
// reducer handler calls: a fact missing its required function_uid is routed
// through partitionDecodeFailures to a visible quarantinedFact (dead-lettered
// on eshu_dp_reducer_input_invalid_facts_total via recordQuarantinedFacts in
// the handler), while every valid sibling still projects — the per-fact
// isolation contract every prior Contract System v1 wave established.
//
// A residual fatal decode error (a type mismatch, or an unsupported schema
// major once these kinds are registered) is returned so the handler fails the
// whole intent through WorkSink.Fail rather than silently truncating rows.
func ExtractCodeTaintEvidenceRowsWithQuarantine(envelopes []facts.Envelope) ([]map[string]any, []quarantinedFact, error) {
	inputs := make([]CodeTaintEvidenceInput, 0, len(envelopes))
	var quarantined []quarantinedFact
	for _, env := range envelopes {
		if env.IsTombstone {
			continue
		}
		input, err := decodeCodeTaintEvidenceInput(env)
		if err != nil {
			q, isQuarantine, fatal := partitionDecodeFailures(env, err)
			if !isQuarantine {
				return nil, nil, fatal
			}
			quarantined = append(quarantined, q)
			continue
		}
		inputs = append(inputs, input)
	}
	return ExtractCodeTaintEvidenceRows(inputs), quarantined, nil
}

// ExtractCodeInterprocEvidenceRowsWithQuarantine is the interproc counterpart
// of ExtractCodeTaintEvidenceRowsWithQuarantine: it decodes each
// code_interproc_evidence envelope, dead-letters a missing required endpoint
// uid as an input_invalid quarantine, and returns the projected TAINT_FLOWS_TO
// edge rows for the valid siblings.
func ExtractCodeInterprocEvidenceRowsWithQuarantine(envelopes []facts.Envelope) ([]map[string]any, []quarantinedFact, error) {
	inputs := make([]CodeInterprocEvidenceInput, 0, len(envelopes))
	var quarantined []quarantinedFact
	for _, env := range envelopes {
		if env.IsTombstone {
			continue
		}
		input, err := decodeCodeInterprocEvidenceInput(env)
		if err != nil {
			q, isQuarantine, fatal := partitionDecodeFailures(env, err)
			if !isQuarantine {
				return nil, nil, fatal
			}
			quarantined = append(quarantined, q)
			continue
		}
		inputs = append(inputs, input)
	}
	return ExtractCodeInterprocEvidenceRows(inputs), quarantined, nil
}

// derefInt dereferences an *int, returning zero for nil.
func derefInt(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

// derefStringTrimmed dereferences a *string and trims it, returning "" for
// nil. Mirrors the pre-Contract-System payloadString helper's universal trim
// for every optional string field decoded through the typed contracts seam.
func derefStringTrimmed(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}
