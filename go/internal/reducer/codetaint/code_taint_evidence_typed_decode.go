// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codetaint

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
)

// DecodeCodeTaintEvidenceInput decodes one code_taint_evidence envelope
// through the contracts seam (schemadecode.DecodeCodeTaintEvidence) and projects it into
// the reducer's CodeTaintEvidenceInput row, RETURNING the decode error rather
// than swallowing it. A missing required function_uid (or any other
// malformed/unsupported-major payload) surfaces as a classified
// *factDecodeError so the caller (ExtractCodeTaintEvidenceRowsWithQuarantine)
// routes it through factdecode.PartitionDecodeFailures and dead-letters it as a visible
// input_invalid quarantine — the accuracy guarantee epic #4566 §1 exists to
// enforce, not the pre-Contract-System silent-empty-string behavior.
//
// Every string field is TrimSpace'd, mirroring the pre-Contract-System
// payloadString helper's universal trim (go/internal/storage/postgres
// secrets_iam_trust_chain_evidence_loader.go): the typed decode seam itself
// does not trim, so a padded function_uid would otherwise flow through
// untrimmed into the graph node attachment key.
//
// Exported (issue #6061) for the reducer root's shared codedataflow
// benchmark (codedataflow_bench_test.go), which measures this decode path
// alongside its function-summary/source/shell-exec siblings that stayed in
// root.
func DecodeCodeTaintEvidenceInput(envelope facts.Envelope) (CodeTaintEvidenceInput, error) {
	evidence, err := schemadecode.DecodeCodeTaintEvidence(envelope)
	if err != nil {
		return CodeTaintEvidenceInput{}, err
	}
	return CodeTaintEvidenceInput{
		FunctionUID:  strings.TrimSpace(evidence.FunctionUID),
		FunctionName: payloadcore.DerefStringTrimmed(evidence.FunctionName),
		RelativePath: payloadcore.DerefStringTrimmed(evidence.RelativePath),
		Language:     payloadcore.DerefStringTrimmed(evidence.Language),
		Kind:         payloadcore.DerefStringTrimmed(evidence.Kind),
		SinkKind:     payloadcore.DerefStringTrimmed(evidence.SinkKind),
		SourceKind:   payloadcore.DerefStringTrimmed(evidence.SourceKind),
		Binding:      payloadcore.DerefStringTrimmed(evidence.Binding),
		SourceLine:   payloadcore.DerefInt(evidence.SourceLine),
		SinkLine:     payloadcore.DerefInt(evidence.SinkLine),
		Confidence:   derefFloat64(evidence.Confidence),
		ClassContext: payloadcore.DerefStringTrimmed(evidence.ClassContext),
		SinkLabel:    payloadcore.DerefStringTrimmed(evidence.SinkLabel),
		SourceLabel:  payloadcore.DerefStringTrimmed(evidence.SourceLabel),
		GuardReason:  payloadcore.DerefStringTrimmed(evidence.GuardReason),
	}, nil
}

// DecodeCodeInterprocEvidenceInput decodes one code_interproc_evidence
// envelope through the contracts seam (schemadecode.DecodeCodeInterprocEvidence) and
// projects it into the reducer's CodeInterprocEvidenceInput row, RETURNING the
// decode error so the caller (ExtractCodeInterprocEvidenceRowsWithQuarantine)
// dead-letters a missing required source_function_uid/sink_function_uid as an
// input_invalid quarantine. Every string field is TrimSpace'd, same rationale
// as DecodeCodeTaintEvidenceInput. Exported for the same reason.
func DecodeCodeInterprocEvidenceInput(envelope facts.Envelope) (CodeInterprocEvidenceInput, error) {
	evidence, err := schemadecode.DecodeCodeInterprocEvidence(envelope)
	if err != nil {
		return CodeInterprocEvidenceInput{}, err
	}
	return CodeInterprocEvidenceInput{
		SourceFunctionUID:  strings.TrimSpace(evidence.SourceFunctionUID),
		SinkFunctionUID:    strings.TrimSpace(evidence.SinkFunctionUID),
		RelativePath:       payloadcore.DerefStringTrimmed(evidence.RelativePath),
		SourceFunctionName: payloadcore.DerefStringTrimmed(evidence.SourceFunctionName),
		SinkFunctionName:   payloadcore.DerefStringTrimmed(evidence.SinkFunctionName),
		Language:           payloadcore.DerefStringTrimmed(evidence.Language),
		SinkKind:           payloadcore.DerefStringTrimmed(evidence.SinkKind),
		SourceKind:         payloadcore.DerefStringTrimmed(evidence.SourceKind),
		Confidence:         derefFloat64(evidence.Confidence),
		Cloud:              payloadcore.DerefBool(evidence.Cloud),
		WhyTrail:           evidence.WhyTrail,
		WhyTrailTruncated:  payloadcore.DerefBool(evidence.WhyTrailTruncated),
	}, nil
}

// ExtractCodeTaintEvidenceRowsWithQuarantine decodes each code_taint_evidence
// envelope through the typed contracts seam and returns the projected graph
// rows plus the per-fact input_invalid quarantines (Contract System v1
// Wave 4f S2, issue #4754). This is the production decode+quarantine path the
// reducer handler calls: a fact missing its required function_uid is routed
// through factdecode.PartitionDecodeFailures to a visible factdecode.QuarantinedFact (dead-lettered
// on eshu_dp_reducer_input_invalid_facts_total via factdecode.RecordQuarantinedFacts in
// the handler), while every valid sibling still projects — the per-fact
// isolation contract every prior Contract System v1 wave established.
//
// A residual fatal decode error (a type mismatch, or an unsupported schema
// major once these kinds are registered) is returned so the handler fails the
// whole intent through WorkSink.Fail rather than silently truncating rows.
func ExtractCodeTaintEvidenceRowsWithQuarantine(envelopes []facts.Envelope) ([]map[string]any, []factdecode.QuarantinedFact, error) {
	inputs := make([]CodeTaintEvidenceInput, 0, len(envelopes))
	var quarantined []factdecode.QuarantinedFact
	for _, env := range envelopes {
		if env.IsTombstone {
			continue
		}
		input, err := DecodeCodeTaintEvidenceInput(env)
		if err != nil {
			q, isQuarantine, fatal := factdecode.PartitionDecodeFailures(env, err)
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
func ExtractCodeInterprocEvidenceRowsWithQuarantine(envelopes []facts.Envelope) ([]map[string]any, []factdecode.QuarantinedFact, error) {
	inputs := make([]CodeInterprocEvidenceInput, 0, len(envelopes))
	var quarantined []factdecode.QuarantinedFact
	for _, env := range envelopes {
		if env.IsTombstone {
			continue
		}
		input, err := DecodeCodeInterprocEvidenceInput(env)
		if err != nil {
			q, isQuarantine, fatal := factdecode.PartitionDecodeFailures(env, err)
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
