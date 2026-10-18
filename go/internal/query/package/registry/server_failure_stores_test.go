// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

import "context"

// failingRegistryGraph fails its failCall-th Run (1-based; 0 fails every
// call, a negative value fails none) with err. Every other Run answers from answers keyed by the exact
// Cypher statement, falling back to rows.
type failingRegistryGraph struct {
	err      error
	failCall int
	answers  map[string][]map[string]any
	rows     []map[string]any
	calls    int
}

func (g *failingRegistryGraph) Run(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
	g.calls++
	if g.failCall == 0 || g.calls == g.failCall {
		return nil, g.err
	}
	if rows, ok := g.answers[cypher]; ok {
		return rows, nil
	}
	return g.rows, nil
}

func (*failingRegistryGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

// failingRegistryCorrelations fails every correlation read with err.
type failingRegistryCorrelations struct{ err error }

func (s failingRegistryCorrelations) ListPackageRegistryCorrelations(
	context.Context, CorrelationFilter,
) (CorrelationPage, error) {
	return CorrelationPage{}, s.err
}
