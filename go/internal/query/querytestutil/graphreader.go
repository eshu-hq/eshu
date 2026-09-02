// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"context"
	"strings"
)

// FakeGraphReader is a graph-read double for handler tests. It satisfies the
// two-method read port handlers depend on, answering from the funcs a caller
// installs instead of reaching a backend.
//
// It lives here rather than in a package query test file because of a Go rule
// that shapes all of epic #6053 (#6060): a symbol declared in a _test.go file
// cannot be imported across a package boundary at all. Each handler family that
// moves out of package query takes its tests with it, and those tests need this
// fake. Leaving it in root would mean every family re-declaring its own copy,
// and a re-declared fake that drifts from the real port is the worst outcome
// available -- it keeps passing while guarding nothing.
//
// The fields are exported, and they have to be: an unexported field is
// unreachable from another package, so a type alias would carry the type but
// not the ability to fill it in. The Fn suffix avoids colliding with the Run
// and RunSingle methods.
//
// The zero value is usable. Many callers construct it empty just to satisfy a
// port, so every nil func answers with no rows rather than panicking.
type FakeGraphReader struct {
	// RunFn answers ordinary Run queries.
	RunFn func(context.Context, string, map[string]any) ([]map[string]any, error)
	// RunIncomingFn answers Run queries that traverse incoming edges. A handler
	// that reads both directions issues two different queries against one
	// reader, so the two need separate answers.
	RunIncomingFn func(context.Context, string, map[string]any) ([]map[string]any, error)
	// RunSingleFn answers RunSingle directly. Leave it nil to fall back to the
	// first row of RunFn.
	RunSingleFn func(context.Context, string, map[string]any) (map[string]any, error)
}

// Run answers a multi-row read, dispatching on the query text because one fake
// stands in for reads a handler issues for different purposes.
//
// An incoming-edge traversal goes to RunIncomingFn, and reports no rows when
// that is nil rather than falling through to RunFn -- falling through would
// hand a test the outgoing rows and let it pass while proving nothing about
// incoming edges.
//
// The dead-code scanner's paged non-function candidate probe is answered with
// no rows, so a test asserting on some other query is not handed those rows by
// accident. Everything else goes to RunFn.
func (f FakeGraphReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	return f.rows(ctx, cypher, params)
}

// RunSingle answers a single-row read. It prefers RunSingleFn so a caller can
// give the two reads different answers, and otherwise takes the first row the
// multi-row dispatch produces. That error propagates rather than surfacing as
// an empty row: tests covering a graph-read failure path depend on seeing it.
func (f FakeGraphReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	if f.RunSingleFn != nil {
		return f.RunSingleFn(ctx, cypher, params)
	}
	rows, err := f.rows(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

// rows holds the query-text dispatch both exported methods answer from. Run
// documents the rules; this is where they live.
//
// The indirection is load-bearing. RunSingle used to fall back by calling Run,
// and routing both through here instead leaves the package with no Run or
// RunSingle call expression at all. That is what lets internal/queryplan's
// production query-callsite inventory walk this directory like any other
// instead of skipping it -- a skip it could only grant by whitelisting the
// exact self-delegation shape, which left a real graph read wearing that shape
// invisible to the gate (#6060, epic #6053). Do not reintroduce the call.
func (f FakeGraphReader) rows(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if strings.Contains(cypher, "incoming_entity_id") {
		if f.RunIncomingFn != nil {
			return f.RunIncomingFn(ctx, cypher, params)
		}
		return nil, nil
	}
	if isDeadCodeNonFunctionCandidateQuery(cypher) {
		return nil, nil
	}
	if f.RunFn == nil {
		return nil, nil
	}
	return f.RunFn(ctx, cypher, params)
}

// isDeadCodeNonFunctionCandidateQuery reports whether cypher is the dead-code
// scanner's paged probe for non-function candidates.
//
// Every marker must be present. Matching on fewer would widen the suppression
// to ordinary queries and blank out rows a test expected to receive, which
// reads as a handler bug rather than a fake one.
func isDeadCodeNonFunctionCandidateQuery(cypher string) bool {
	if !strings.Contains(cypher, "RETURN coalesce(e.uid, e.id) as entity_id") ||
		!strings.Contains(cypher, "SKIP $skip") ||
		!strings.Contains(cypher, "LIMIT $limit") {
		return false
	}
	return strings.Contains(cypher, "e:Class") ||
		strings.Contains(cypher, "e:Struct") ||
		strings.Contains(cypher, "e:Interface")
}
