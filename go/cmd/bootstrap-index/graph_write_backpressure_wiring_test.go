// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graphbackpressure"
)

// TestNewBootstrapCanonicalGateDisabledByDefault proves the default-off
// contract: with both ESHU_GRAPH_WRITE_MAX_IN_FLIGHT and
// ESHU_GRAPH_WRITE_CANONICAL_MAX_IN_FLIGHT unset, the gate is nil so
// graphbackpressure.WrapExecutorWithGate is a passthrough.
func TestNewBootstrapCanonicalGateDisabledByDefault(t *testing.T) {
	t.Parallel()

	gate := newBootstrapCanonicalGate(func(string) string { return "" }, nil)
	if gate != nil {
		t.Fatalf("newBootstrapCanonicalGate() = %v, want nil for unset env", gate)
	}
}

// TestNewBootstrapCanonicalGateUsesCanonicalEnvFirst proves
// ESHU_GRAPH_WRITE_CANONICAL_MAX_IN_FLIGHT takes precedence over the shared
// ESHU_GRAPH_WRITE_MAX_IN_FLIGHT fallback.
func TestNewBootstrapCanonicalGateUsesCanonicalEnvFirst(t *testing.T) {
	t.Parallel()

	gate := newBootstrapCanonicalGate(func(key string) string {
		switch key {
		case graphbackpressure.CanonicalMaxInFlightEnv:
			return "5"
		case graphbackpressure.MaxInFlightEnv:
			return "9"
		default:
			return ""
		}
	}, nil)
	if gate == nil {
		t.Fatal("newBootstrapCanonicalGate() = nil, want a bounded gate")
	}
	if got, want := gate.MaxInFlight(), 5; got != want {
		t.Fatalf("gate.MaxInFlight() = %d, want %d (canonical env must win over shared)", got, want)
	}
}

// TestNewBootstrapCanonicalGateFallsBackToSharedEnv proves the shared
// ESHU_GRAPH_WRITE_MAX_IN_FLIGHT knob still bounds bootstrap-index's canonical
// class when the per-class env is unset, matching the reducer's fallback
// shape.
func TestNewBootstrapCanonicalGateFallsBackToSharedEnv(t *testing.T) {
	t.Parallel()

	gate := newBootstrapCanonicalGate(func(key string) string {
		if key == graphbackpressure.MaxInFlightEnv {
			return "3"
		}
		return ""
	}, nil)
	if gate == nil {
		t.Fatal("newBootstrapCanonicalGate() = nil, want a bounded gate")
	}
	if got, want := gate.MaxInFlight(), 3; got != want {
		t.Fatalf("gate.MaxInFlight() = %d, want %d (shared fallback)", got, want)
	}
}
