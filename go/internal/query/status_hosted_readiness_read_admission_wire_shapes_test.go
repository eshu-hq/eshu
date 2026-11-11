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

// The pin must stay satisfiable by the production graph reader: if the
// re-declared Run/RunSingle shape drifts from what production serves, the
// satellite contract silently stops matching the route's actual reads.
// This assertion fails compilation on either side's drift.
var _ StatusHostedReadinessReadAdmission = (*Neo4jReader)(nil)

// TestStatusHostedReadinessReadAdmissionWireShapesAreSatelliteConsumable
// proves the wire shapes behind the #9669 Status hosted-readiness pin: any
// shared-port graph reader satisfies the admission, and both re-declared
// reads fail closed (never panic, never return confident emptiness) when the
// guarded graph is unavailable — the repository-count leg through RunSingle
// and the workload-leftovers detection leg through Run. The stub legs prove
// the seam needs nothing wider than a GraphQuery; the production legs run
// the same reads through the production reader with no driver wired, so a
// drift that breaks the production path fails here instead of passing
// against the stub alone.
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

	// The fail-closed legs above run a stub, so on their own they cannot
	// catch production drift: any error, including one the stub invents,
	// would satisfy them. These legs run the same pinned reads through the
	// production reader with no driver wired (a normal production shape per
	// GraphConfigured): the error must come from production code, so a
	// probe-matching error here would mean the stub leaked into the
	// production path rather than the reverse.
	production := bindStatusHostedReadinessPin(NewNeo4jReader(nil, "test-db"))
	if _, err := production.RunSingle(ctx, "MATCH (r:Repository) RETURN count(r) as count", nil); err == nil {
		t.Fatal("RunSingle through the production reader with no driver = nil error, want the production unavailable-graph error")
	} else if errors.Is(err, errStatusHostedReadinessWireShapeProbe) {
		t.Fatalf("RunSingle through the production reader returned the test probe error %v: the stub leaked into the production path", err)
	}
	if _, err := production.Run(ctx, "MATCH (w:Workload) RETURN w LIMIT 1", nil); err == nil {
		t.Fatal("Run through the production reader with no driver = nil error, want the production unavailable-graph error")
	} else if errors.Is(err, errStatusHostedReadinessWireShapeProbe) {
		t.Fatalf("Run through the production reader returned the test probe error %v: the stub leaked into the production path", err)
	}
}
