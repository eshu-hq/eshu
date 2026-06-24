// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

// LoadCodeFunctionGraphIDs implements the reducer's FunctionID->uid loader by
// scanning code_function_summary facts for one scope generation and mapping each
// FunctionID to the graph_uid the collector resolved. Functions whose uid did not
// resolve are returned with an empty uid so the replacement writer can clear any
// stale mapping from an earlier generation.
func (s FactStore) LoadCodeFunctionGraphIDs(
	ctx context.Context,
	scopeID string,
	generationID string,
) (map[summary.FunctionID]string, error) {
	envelopes, err := s.ListFactsByKind(ctx, scopeID, generationID, []string{facts.CodeFunctionSummaryFactKind})
	if err != nil {
		return nil, err
	}
	ids := make(map[summary.FunctionID]string, len(envelopes))
	for _, envelope := range envelopes {
		if envelope.IsTombstone {
			continue
		}
		id := payloadString(envelope.Payload, "function_id")
		uid := payloadString(envelope.Payload, "graph_uid")
		if id == "" {
			continue
		}
		ids[summary.FunctionID(id)] = uid
	}
	return ids, nil
}
