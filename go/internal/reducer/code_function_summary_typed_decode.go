// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

// codeFunctionSummaryEffects decodes one code_function_summary envelope
// through the contracts seam (decodeCodeFunctionSummary) and reconstructs the
// function's summary.Effects, returning the durable summary.FunctionID and the
// decode error (so the caller routes a missing required function_id through
// partitionDecodeFailures to an input_invalid dead-letter, not a silent drop —
// the accuracy guarantee epic #4566 §1 enforces). A decode SUCCESS with a
// present-but-blank function_id returns ok=false without an error: that is a
// valid-but-empty-identity observation the pre-Contract-System
// `if id == "" { continue }` guard dropped, not a malformed payload, so it is
// skipped (not dead-lettered).
//
// param_to_call_arg[].callee is TrimSpace'd before it becomes a
// summary.FunctionID, mirroring the pre-Contract-System payloadString read:
// the durable summary must key its callee edge on the same trimmed FunctionID
// the fixpoint's summary/graph-id maps use, or a padded callee points at a
// function the fixpoint cannot match (Codex review, PR #4758).
func codeFunctionSummaryEffects(envelope facts.Envelope) (summary.FunctionID, summary.Effects, bool, error) {
	typed, err := decodeCodeFunctionSummary(envelope)
	if err != nil {
		return "", summary.Effects{}, false, err
	}
	functionID := strings.TrimSpace(typed.FunctionID)
	if functionID == "" {
		return "", summary.Effects{}, false, nil
	}
	effects := summary.Effects{
		ParamToReturn:  typed.ParamToReturn,
		SourceToReturn: typed.SourceToReturn,
	}
	for _, sink := range typed.ParamToSink {
		effects.ParamToSink = append(effects.ParamToSink, summary.ParamSink{
			Param:    sink.Param,
			SinkKind: strings.TrimSpace(sink.SinkKind),
		})
	}
	for _, flow := range typed.ParamToCallArg {
		effects.ParamToCallArg = append(effects.ParamToCallArg, summary.CallArgFlow{
			Callee: summary.FunctionID(strings.TrimSpace(flow.Callee)),
			Param:  flow.Param,
			Arg:    flow.Arg,
		})
	}
	return summary.FunctionID(functionID), effects, true, nil
}

// codeFunctionGraphID decodes one code_function_summary envelope and returns
// the durable summary.FunctionID plus the graph_uid the collector resolved
// (empty when unresolved), an ok flag, and the decode error. Same
// error/blank-skip contract as codeFunctionSummaryEffects.
func codeFunctionGraphID(envelope facts.Envelope) (summary.FunctionID, string, bool, error) {
	typed, err := decodeCodeFunctionSummary(envelope)
	if err != nil {
		return "", "", false, err
	}
	functionID := strings.TrimSpace(typed.FunctionID)
	if functionID == "" {
		return "", "", false, nil
	}
	return summary.FunctionID(functionID), derefStringTrimmed(typed.GraphUID), true, nil
}

// codeFunctionSource decodes one code_function_source envelope into an
// interproc.Source entry port, returning an ok flag and the decode error. A
// missing required function_id or kind surfaces as a decode error (routed to
// an input_invalid dead-letter by the caller); a present-but-blank
// function_id/kind returns ok=false without an error (a valid-but-empty
// observation the pre-Contract-System `if id == "" || kind == "" { continue }`
// guard dropped).
func codeFunctionSource(envelope facts.Envelope) (interproc.Source, bool, error) {
	typed, err := decodeCodeFunctionSource(envelope)
	if err != nil {
		return interproc.Source{}, false, err
	}
	functionID := strings.TrimSpace(typed.FunctionID)
	kind := strings.TrimSpace(typed.Kind)
	if functionID == "" || kind == "" {
		return interproc.Source{}, false, nil
	}
	return interproc.Source{
		Port: interproc.Port{
			Func: interproc.FunctionID(functionID),
			Slot: interproc.Slot{Kind: interproc.SlotParam, Index: derefInt(typed.ParamIndex)},
		},
		Kind: kind,
	}, true, nil
}

// ExtractCodeFunctionSummaryEffectsWithQuarantine decodes each
// code_function_summary envelope into the FunctionID->Effects map the handler
// upserts, plus the per-fact input_invalid quarantines (Contract System v1
// Wave 4f S2, issue #4754). A fact missing its required function_id is routed
// through partitionDecodeFailures to a visible quarantinedFact rather than
// being silently skipped, while every valid sibling still enters the map. A
// residual fatal decode error (a type mismatch or unsupported schema major) is
// returned so the handler fails the whole intent.
func ExtractCodeFunctionSummaryEffectsWithQuarantine(envelopes []facts.Envelope) (map[summary.FunctionID]summary.Effects, []quarantinedFact, error) {
	out := make(map[summary.FunctionID]summary.Effects, len(envelopes))
	var quarantined []quarantinedFact
	for _, env := range envelopes {
		if env.IsTombstone {
			continue
		}
		id, effects, ok, err := codeFunctionSummaryEffects(env)
		if err != nil {
			q, isQuarantine, fatal := partitionDecodeFailures(env, err)
			if !isQuarantine {
				return nil, nil, fatal
			}
			quarantined = append(quarantined, q)
			continue
		}
		if !ok {
			continue
		}
		out[id] = effects
	}
	return out, quarantined, nil
}

// ExtractCodeFunctionGraphIDsWithQuarantine decodes each code_function_summary
// envelope into the FunctionID->graph-uid map, plus the per-fact quarantines.
// It reads the SAME code_function_summary facts as
// ExtractCodeFunctionSummaryEffectsWithQuarantine; the handler quarantines the
// summary-effects view once and discards this function's quarantines to avoid
// double-counting the same malformed fact on the input_invalid counter.
func ExtractCodeFunctionGraphIDsWithQuarantine(envelopes []facts.Envelope) (map[summary.FunctionID]string, []quarantinedFact, error) {
	out := make(map[summary.FunctionID]string, len(envelopes))
	var quarantined []quarantinedFact
	for _, env := range envelopes {
		if env.IsTombstone {
			continue
		}
		id, uid, ok, err := codeFunctionGraphID(env)
		if err != nil {
			q, isQuarantine, fatal := partitionDecodeFailures(env, err)
			if !isQuarantine {
				return nil, nil, fatal
			}
			quarantined = append(quarantined, q)
			continue
		}
		if !ok {
			continue
		}
		out[id] = uid
	}
	return out, quarantined, nil
}

// ExtractCodeFunctionSourcesWithQuarantine decodes each code_function_source
// envelope into the interproc.Source slice plus the per-fact quarantines.
func ExtractCodeFunctionSourcesWithQuarantine(envelopes []facts.Envelope) ([]interproc.Source, []quarantinedFact, error) {
	sources := make([]interproc.Source, 0, len(envelopes))
	var quarantined []quarantinedFact
	for _, env := range envelopes {
		if env.IsTombstone {
			continue
		}
		source, ok, err := codeFunctionSource(env)
		if err != nil {
			q, isQuarantine, fatal := partitionDecodeFailures(env, err)
			if !isQuarantine {
				return nil, nil, fatal
			}
			quarantined = append(quarantined, q)
			continue
		}
		if !ok {
			continue
		}
		sources = append(sources, source)
	}
	return sources, quarantined, nil
}
