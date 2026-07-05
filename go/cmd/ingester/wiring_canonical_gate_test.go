// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graphbackpressure"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// bareIngesterExecutor is a minimal raw Executor (Execute only) standing in for
// the Bolt driver executor; the wiring layers InstrumentedExecutor /
// RetryingExecutor / TimeoutExecutor over it, which supply GroupExecutor.
type bareIngesterExecutor struct{}

func (bareIngesterExecutor) Execute(context.Context, sourcecypher.Statement) error { return nil }

// drainCapableExecutor is a raw executor that also satisfies retractDrainReader,
// standing in for the Bolt executor whose RunWrite drives the full-refresh
// DETACH DELETE drain loop.
type drainCapableExecutor struct{}

func (drainCapableExecutor) Execute(context.Context, sourcecypher.Statement) error { return nil }
func (drainCapableExecutor) RunWrite(context.Context, string, map[string]any) (DrainWriteResult, error) {
	return DrainWriteResult{}, nil
}

func newTestNornicDBCanonicalExecutorWithRaw(raw sourcecypher.Executor, gate *sourcecypher.BackpressureGate) sourcecypher.Executor {
	return canonicalExecutorForGraphBackend(
		raw,
		runtimecfg.GraphBackendNornicDB,
		0,
		false,
		defaultNornicDBPhaseGroupStatements,
		defaultNornicDBFilePhaseStatements,
		defaultNornicDBEntityPhaseStatements,
		nil,
		4,
		defaultNornicDBCanonicalRetractBatchSize,
		nil,
		nil,
		gate,
	)
}

func newTestNornicDBCanonicalExecutor(gate *sourcecypher.BackpressureGate) sourcecypher.Executor {
	return canonicalExecutorForGraphBackend(
		bareIngesterExecutor{},
		runtimecfg.GraphBackendNornicDB,
		0,
		false,
		defaultNornicDBPhaseGroupStatements,
		defaultNornicDBFilePhaseStatements,
		defaultNornicDBEntityPhaseStatements,
		nil,
		4,
		defaultNornicDBCanonicalRetractBatchSize,
		nil,
		nil,
		gate,
	)
}

// TestCanonicalExecutorGatesInnerLayerAndPreservesPhaseGroup is the #4729
// regression: the graph-write gate must bound the INNER GroupExecutor layer
// (each concurrent ExecuteGroup in the phase-group fan-out) WITHOUT stripping
// the outer nornicDBPhaseGroupExecutor's PhaseGroupExecutor capability. An
// earlier revision wrapped the outer phase-group executor, which — because it
// is a PhaseGroupExecutor, not a GroupExecutor — got demoted to an Execute-only
// wrapper, silently regressing the canonical writer to per-statement sequential
// writes the moment the gate was enabled.
func TestCanonicalExecutorGatesInnerLayerAndPreservesPhaseGroup(t *testing.T) {
	getenv := func(name string) string {
		if name == graphbackpressure.MaxInFlightEnv {
			return "8"
		}
		return ""
	}
	executor := newTestNornicDBCanonicalExecutor(newIngesterCanonicalGate(getenv, nil))

	// The outer executor must still be the phase-group executor (capability
	// preserved) so NewCanonicalNodeWriter.Write takes the batched phase path.
	pge, ok := executor.(nornicDBPhaseGroupExecutor)
	if !ok {
		t.Fatalf("gated executor type = %T, want nornicDBPhaseGroupExecutor (phase-group capability lost)", executor)
	}
	// Its INNER layer (which every concurrent ExecuteGroup in the fan-out draws
	// from) must be the backpressure-gated executor, so the ceiling bounds actual
	// concurrent backend writes, not merely outer phase-group calls.
	if _, gated := pge.inner.(*sourcecypher.BackpressureExecutor); !gated {
		t.Fatalf("gated executor inner = %T, want *sourcecypher.BackpressureExecutor (fan-out not bounded)", pge.inner)
	}
	// The gated inner must still satisfy GroupExecutor so ExecuteGroup works.
	if _, ok := pge.inner.(sourcecypher.GroupExecutor); !ok {
		t.Fatalf("gated inner = %T, want a GroupExecutor (ExecuteGroup stripped)", pge.inner)
	}
}

// TestCanonicalExecutorPassthroughWhenUnset proves that with no ceiling
// configured the inner layer is unchanged (nil gate = passthrough): zero
// behavior change for a deployment that has not opted into backpressure.
func TestCanonicalExecutorPassthroughWhenUnset(t *testing.T) {
	executor := newTestNornicDBCanonicalExecutor(newIngesterCanonicalGate(func(string) string { return "" }, nil))
	pge, ok := executor.(nornicDBPhaseGroupExecutor)
	if !ok {
		t.Fatalf("executor type = %T, want nornicDBPhaseGroupExecutor", executor)
	}
	if _, gated := pge.inner.(*sourcecypher.BackpressureExecutor); gated {
		t.Fatalf("unset ceiling: inner is backpressure-gated, want the ungated TimeoutExecutor passthrough")
	}
}

// TestCanonicalExecutorGatesViaCanonicalClassEnv proves the per-class override
// (ESHU_GRAPH_WRITE_CANONICAL_MAX_IN_FLIGHT, #4448) also enables the inner gate.
func TestCanonicalExecutorGatesViaCanonicalClassEnv(t *testing.T) {
	getenv := func(name string) string {
		if name == graphbackpressure.CanonicalMaxInFlightEnv {
			return "2"
		}
		return ""
	}
	executor := newTestNornicDBCanonicalExecutor(newIngesterCanonicalGate(getenv, nil))
	pge, ok := executor.(nornicDBPhaseGroupExecutor)
	if !ok {
		t.Fatalf("executor type = %T, want nornicDBPhaseGroupExecutor", executor)
	}
	if _, gated := pge.inner.(*sourcecypher.BackpressureExecutor); !gated {
		t.Fatalf("canonical-class ceiling: inner = %T, want *sourcecypher.BackpressureExecutor", pge.inner)
	}
}

// TestCanonicalExecutorGatesDrainWritesWhenConfigured proves the #4729 review
// fix: the full-refresh DETACH DELETE drain path (drainReader.RunWrite, which
// bypasses the gated inner GroupExecutor) is also bound to the gate, so
// concurrent drain writes cannot exceed ESHU_GRAPH_WRITE_MAX_IN_FLIGHT.
func TestCanonicalExecutorGatesDrainWritesWhenConfigured(t *testing.T) {
	getenv := func(name string) string {
		if name == graphbackpressure.MaxInFlightEnv {
			return "8"
		}
		return ""
	}
	executor := newTestNornicDBCanonicalExecutorWithRaw(drainCapableExecutor{}, newIngesterCanonicalGate(getenv, nil))
	pge, ok := executor.(nornicDBPhaseGroupExecutor)
	if !ok {
		t.Fatalf("executor type = %T, want nornicDBPhaseGroupExecutor", executor)
	}
	if _, gated := pge.drainReader.(gatedDrainReader); !gated {
		t.Fatalf("gated drain reader = %T, want gatedDrainReader (drain writes bypass the gate)", pge.drainReader)
	}
}

// TestCanonicalExecutorDrainPassthroughWhenUnset proves the drain path stays the
// raw reader (ungated passthrough) when no ceiling is configured.
func TestCanonicalExecutorDrainPassthroughWhenUnset(t *testing.T) {
	executor := newTestNornicDBCanonicalExecutorWithRaw(drainCapableExecutor{}, newIngesterCanonicalGate(func(string) string { return "" }, nil))
	pge, ok := executor.(nornicDBPhaseGroupExecutor)
	if !ok {
		t.Fatalf("executor type = %T, want nornicDBPhaseGroupExecutor", executor)
	}
	if _, gated := pge.drainReader.(gatedDrainReader); gated {
		t.Fatalf("unset ceiling: drain reader is gated, want the raw passthrough reader")
	}
}
