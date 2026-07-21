// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

// shared.go re-exports the pure assertion core that now lives in the importable
// internal/goldengate package. The B-7 golden-corpus gate keeps its
// I/O-and-orchestration layer (graph.go, drains.go, query.go, mcp.go,
// runner.go, timing.go, main.go) in this command package, but every
// snapshot-contract type and every Evaluate* assertion is defined once in
// goldengate so the out-of-tree contributor conformance suite
// (go/conformance, issue #4112 / R-10) asserts against the *same* logic with no
// forked copy. These aliases let the existing gate call sites and tests
// reference the short package-local names unchanged.

import gg "github.com/eshu-hq/eshu/go/internal/goldengate"

// Snapshot-contract types (typed view of the B-12 golden snapshot).
type (
	// Snapshot is the typed golden-snapshot contract.
	Snapshot = gg.Snapshot
	// GraphSnapshot holds the per-label/per-edge tolerances and required
	// correlations/nodes.
	GraphSnapshot = gg.GraphSnapshot
	// CountRange is an inclusive [Min,Max] tolerance.
	CountRange = gg.CountRange
	// RequiredCorrelation is an existence-style correlation assertion.
	RequiredCorrelation = gg.RequiredCorrelation
	// RequiredNode is an existence-plus-property node assertion.
	RequiredNode = gg.RequiredNode
	// RequiredSelfLoop is a bounded self-loop count assertion (the self-loop-axis
	// counterpart to RequiredCorrelation/RequiredNode).
	RequiredSelfLoop = gg.RequiredSelfLoop
	// DrainAssertions captures the B-7(a) queue-drain bounds.
	DrainAssertions = gg.DrainAssertions
	// DrainBound carries one queue's tolerated nonterminal/residual ceiling.
	DrainBound = gg.DrainBound
	// DrainCounts is the observed queue state at a drain poll.
	DrainCounts = gg.DrainCounts
	// QueryShapes describes the canonical MCP and HTTP response contracts.
	QueryShapes = gg.QueryShapes
	// QueryShape declares the required fields and minimum results for one query.
	QueryShape = gg.QueryShape
	// Finding is a single gate assertion outcome.
	Finding = gg.Finding
	// Report accumulates findings across all gate phases.
	Report = gg.Report
)

// Pure assertion entry points, re-exported as package-local names. Each is the
// single source of truth in goldengate; the gate and the conformance suite both
// call these, never a copy.
var (
	// LoadSnapshot reads and parses a golden/spec snapshot file.
	LoadSnapshot = gg.LoadSnapshot
	// EvaluateDrains turns observed drain counts into required findings.
	EvaluateDrains = gg.EvaluateDrains
	// EvaluateRequiredCorrelation produces an existence-style correlation finding.
	EvaluateRequiredCorrelation = gg.EvaluateRequiredCorrelation
	// EvaluateEdgeProperty produces a required edge-property finding.
	EvaluateEdgeProperty = gg.EvaluateEdgeProperty
	// EvaluateNodeProperty produces a required node-property finding.
	EvaluateNodeProperty = gg.EvaluateNodeProperty
	// EvaluateRequiredNode produces a required node-existence finding.
	EvaluateRequiredNode = gg.EvaluateRequiredNode
	// EvaluateRequiredSelfLoop produces a bounded self-loop count finding.
	EvaluateRequiredSelfLoop = gg.EvaluateRequiredSelfLoop
	// EvaluateNodePresent produces a node-present smoke finding.
	EvaluateNodePresent = gg.EvaluateNodePresent
	// EvaluateNodeCount compares an observed node count to its tolerance.
	EvaluateNodeCount = gg.EvaluateNodeCount
	// EvaluateEdgeCount compares an observed edge count to its tolerance.
	EvaluateEdgeCount = gg.EvaluateEdgeCount
	// EvaluateQueryShape validates a raw JSON response against a query shape.
	EvaluateQueryShape = gg.EvaluateQueryShape
	// EvaluateQuerySurfaceParity validates offline API/MCP/CLI parity metadata.
	EvaluateQuerySurfaceParity = gg.EvaluateQuerySurfaceParity
	// EvaluateTiming produces a required wall-time finding.
	EvaluateTiming = gg.EvaluateTiming
)
