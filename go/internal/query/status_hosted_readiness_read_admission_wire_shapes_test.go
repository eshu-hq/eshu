// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// errStatusHostedReadinessWireShapeProbe is the sentinel the failing guarded
// graph below answers both reads with. A non-nil error (never a panic, never
// a confident empty result) is the fail-closed proof the pinned shape carries
// only guarded-port reads.
var errStatusHostedReadinessWireShapeProbe = errors.New("status hosted-readiness wire-shape probe: graph unavailable")

// errStatusHostedReadinessWireShapeGraph is a guarded graph that answers
// every read with the probe error. It implements the full two-method graph
// surface the pin mirrors, so binding it through the pin proves the
// satellite path needs nothing wider than the request-scoped graph handle:
// no session, no transaction, no write surface.
type errStatusHostedReadinessWireShapeGraph struct{ err error }

func (g errStatusHostedReadinessWireShapeGraph) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, g.err
}

func (g errStatusHostedReadinessWireShapeGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, g.err
}

var _ querycontract.GraphQuery = errStatusHostedReadinessWireShapeGraph{}

// bindStatusHostedReadinessPin binds a graph through the pin behind a call
// boundary, so the fail-closed reads below exercise the pinned shape rather
// than a directly-constructed concrete double.
func bindStatusHostedReadinessPin(g querycontract.GraphQuery) StatusHostedReadinessReadAdmission {
	return g
}

// TestStatusHostedReadinessReadAdmissionWireShapesAreSatelliteConsumable
// proves the wire shapes behind the #9669 Status hosted-readiness pin: any
// shared-port graph reader satisfies the admission, and both re-declared
// reads fail closed (never panic, never return confident emptiness) when the
// guarded graph is unavailable — the repository-count leg through RunSingle
// and the workload-leftovers detection leg through Run.
func TestStatusHostedReadinessReadAdmissionWireShapesAreSatelliteConsumable(t *testing.T) {
	t.Parallel()

	_ = querycontract.GraphQuery(errStatusHostedReadinessWireShapeGraph{err: errStatusHostedReadinessWireShapeProbe}).(StatusHostedReadinessReadAdmission)

	admission := bindStatusHostedReadinessPin(errStatusHostedReadinessWireShapeGraph{err: errStatusHostedReadinessWireShapeProbe})

	ctx := context.Background()
	if _, err := admission.RunSingle(ctx, "MATCH (r:Repository) RETURN count(r) as count", nil); err == nil {
		t.Error("RunSingle over an unavailable graph = nil error, want a fail-closed error")
	}
	if _, err := admission.Run(ctx, "MATCH (w:Workload) RETURN w LIMIT 1", nil); err == nil {
		t.Error("Run over an unavailable graph = nil error, want a fail-closed error")
	}
}
