// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
)

// LoadCodeFunctionSources implements the reducer's function-source loader by
// scanning code_function_source facts for one scope generation and rebuilding
// each as an interproc source port, keyed by the durable FunctionID. JSONB
// numeric scans yield float64, so the parameter index is coerced here.
func (s FactStore) LoadCodeFunctionSources(
	ctx context.Context,
	scopeID string,
	generationID string,
) ([]interproc.Source, error) {
	envelopes, err := s.ListFactsByKind(ctx, scopeID, generationID, []string{facts.CodeFunctionSourceFactKind})
	if err != nil {
		return nil, err
	}
	sources := make([]interproc.Source, 0, len(envelopes))
	for _, envelope := range envelopes {
		if envelope.IsTombstone {
			continue
		}
		id := payloadString(envelope.Payload, "function_id")
		kind := payloadString(envelope.Payload, "kind")
		if id == "" || kind == "" {
			continue
		}
		sources = append(sources, interproc.Source{
			Port: interproc.Port{
				Func: interproc.FunctionID(id),
				Slot: interproc.Slot{Kind: interproc.SlotParam, Index: payloadInt(envelope.Payload, "param_index")},
			},
			Kind: kind,
		})
	}
	return sources, nil
}
