// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

// LoadCodeFunctionSummaryEffects implements the reducer's function-summary loader
// by scanning code_function_summary facts for one scope generation and rebuilding
// each function's summary.Effects from its payload, keyed by the durable
// FunctionID. JSONB numeric scans yield float64, so the integer fields are
// coerced here.
func (s FactStore) LoadCodeFunctionSummaryEffects(
	ctx context.Context,
	scopeID string,
	generationID string,
) (map[summary.FunctionID]summary.Effects, error) {
	envelopes, err := s.ListFactsByKind(ctx, scopeID, generationID, []string{facts.CodeFunctionSummaryFactKind})
	if err != nil {
		return nil, err
	}
	out := make(map[summary.FunctionID]summary.Effects, len(envelopes))
	for _, envelope := range envelopes {
		if envelope.IsTombstone {
			continue
		}
		id := payloadString(envelope.Payload, "function_id")
		if id == "" {
			continue
		}
		out[summary.FunctionID(id)] = codeFunctionSummaryEffectsFromPayload(envelope.Payload)
	}
	return out, nil
}

// codeFunctionSummaryEffectsFromPayload rebuilds summary.Effects from one fact
// payload, coercing the JSONB float64/any shapes back to the typed effect lists.
func codeFunctionSummaryEffectsFromPayload(payload map[string]any) summary.Effects {
	effects := summary.Effects{
		ParamToReturn:  payloadIntSlice(payload, "param_to_return"),
		SourceToReturn: payloadStringSlice(payload, "source_to_return"),
	}
	for _, raw := range payloadAnySlice(payload, "param_to_sink") {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		effects.ParamToSink = append(effects.ParamToSink, summary.ParamSink{
			Param:    payloadInt(entry, "param"),
			SinkKind: payloadString(entry, "sink_kind"),
		})
	}
	for _, raw := range payloadAnySlice(payload, "param_to_call_arg") {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		effects.ParamToCallArg = append(effects.ParamToCallArg, summary.CallArgFlow{
			Callee: summary.FunctionID(payloadString(entry, "callee")),
			Param:  payloadInt(entry, "param"),
			Arg:    payloadInt(entry, "arg"),
		})
	}
	return effects
}

// payloadAnySlice reads a slice payload field as []any (JSONB arrays scan to []any).
func payloadAnySlice(payload map[string]any, key string) []any {
	value, _ := payload[key].([]any)
	return value
}

// payloadInt reads an integer payload field, coercing the float64 that a JSONB
// numeric scan yields.
func payloadInt(payload map[string]any, key string) int {
	switch value := payload[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case int64:
		return int(value)
	}
	return 0
}

// payloadIntSlice reads an integer slice payload field with JSONB float64 coercion.
func payloadIntSlice(payload map[string]any, key string) []int {
	raw := payloadAnySlice(payload, key)
	if len(raw) == 0 {
		return nil
	}
	out := make([]int, 0, len(raw))
	for _, v := range raw {
		switch n := v.(type) {
		case float64:
			out = append(out, int(n))
		case int:
			out = append(out, n)
		case int64:
			out = append(out, int(n))
		}
	}
	return out
}
