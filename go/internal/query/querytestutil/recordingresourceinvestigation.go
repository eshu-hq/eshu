// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"context"
	"strings"
	"sync"
)

// ResourceInvestigationRunCall records one Run query the
// RecordingResourceInvestigationGraph answered, in call order.
type ResourceInvestigationRunCall struct {
	Cypher string
	Params map[string]any
}

// RecordingResourceInvestigationGraph is a graph-read double for the
// resource-investigation family. It answers the family's four directed reads
// (instance workloads, workload rows, incoming and outgoing edges) from
// caller-installed row sets, serves queued default rows in order, and records
// every Run query in RunCalls so tests can assert which reads a handler
// issued. The zero value is usable. Not safe for concurrent use beyond the
// mutex-guarded recording: callers construct one per test.
type RecordingResourceInvestigationGraph struct {
	mu                   sync.Mutex
	RunCalls             []ResourceInvestigationRunCall
	RunRows              [][]map[string]any
	WorkloadRows         []map[string]any
	InstanceWorkloadRows []map[string]any
	IncomingRows         []map[string]any
	OutgoingRows         []map[string]any
	WorkloadErr          error
	InstanceWorkloadErr  error
	IncomingErr          error
	OutgoingErr          error
	SelectorLabel        string
}

// Run answers a multi-row read, dispatching on the query text the way the
// resource-investigation production code shapes it, and records the call.
func (g *RecordingResourceInvestigationGraph) Run(
	ctx context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	_ = ctx
	g.mu.Lock()
	defer g.mu.Unlock()
	g.RunCalls = append(g.RunCalls, ResourceInvestigationRunCall{Cypher: cypher, Params: params})
	switch {
	case strings.Contains(cypher, "-[:INSTANCE_OF]->(workload:Workload)"):
		if g.InstanceWorkloadErr != nil {
			return nil, g.InstanceWorkloadErr
		}
		return g.InstanceWorkloadRows, nil
	case strings.Contains(cypher, "MATCH (instance:WorkloadInstance)"):
		if g.WorkloadErr != nil {
			return nil, g.WorkloadErr
		}
		return g.WorkloadRows, nil
	case strings.Contains(cypher, "<-[rels"):
		if g.IncomingErr != nil {
			return nil, g.IncomingErr
		}
		return g.IncomingRows, nil
	case strings.Contains(cypher, "-[rels"):
		if g.OutgoingErr != nil {
			return nil, g.OutgoingErr
		}
		return g.OutgoingRows, nil
	}
	if g.SelectorLabel != "" && !strings.Contains(cypher, "MATCH (n:"+g.SelectorLabel+")") {
		return nil, nil
	}
	if len(g.RunRows) == 0 {
		return nil, nil
	}
	rows := g.RunRows[0]
	g.RunRows = g.RunRows[1:]
	return rows, nil
}

// RunSingle answers a single-row read with no row. The production paths this
// double serves never issue narrow single-row reads in the tests that use
// it; returning an error here would fail those tests for a path they do not
// exercise.
func (g *RecordingResourceInvestigationGraph) RunSingle(
	context.Context,
	string,
	map[string]any,
) (map[string]any, error) {
	return nil, nil
}
