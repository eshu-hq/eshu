// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	resultshape "github.com/eshu-hq/eshu/go/internal/mcp/result/shape"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// benchShape names one result shape the render benchmark runs.
type benchShape struct {
	name string
	mode resultshape.Mode
}

// benchShapes lists the two result shapes. The benchmark in
// render_budget_bench_test.go uses only this table and newBenchResult, so the
// same file also compiles on a tree that predates the shape option.
var benchShapes = []benchShape{
	{"dual", resultshape.Dual},
	{"structured", resultshape.Structured},
}

// newBenchResult builds a dispatch result carrying envelope in shape.
func newBenchResult(envelope *query.ResponseEnvelope, shape benchShape) *dispatchResult {
	return &dispatchResult{Value: envelope, Envelope: envelope, Shape: shape.mode}
}
