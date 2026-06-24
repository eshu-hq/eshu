// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package askwiring_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/engine"
	"github.com/eshu-hq/eshu/go/internal/askwiring"
)

// envFunc builds a getenv closure from a fixed map for table tests.
func envFunc(vals map[string]string) func(string) string {
	return func(key string) string {
		return vals[key]
	}
}

// TestResolveEngineOptionsDefaults proves that with no budget env vars set,
// ResolveEngineOptions returns the documented engine defaults. This is the
// safe-default contract: operators who set nothing keep the conservative
// MaxIterations / MaxToolCallsPerTurn values.
func TestResolveEngineOptionsDefaults(t *testing.T) {
	t.Parallel()

	def := engine.DefaultOptions()
	opts := askwiring.ResolveEngineOptions(envFunc(nil), nil)

	if opts.MaxIterations != def.MaxIterations {
		t.Errorf("MaxIterations = %d, want default %d", opts.MaxIterations, def.MaxIterations)
	}
	if opts.MaxToolCallsPerTurn != def.MaxToolCallsPerTurn {
		t.Errorf("MaxToolCallsPerTurn = %d, want default %d", opts.MaxToolCallsPerTurn, def.MaxToolCallsPerTurn)
	}
}

// TestResolveEngineOptionsOverrides proves that valid positive env values
// override the defaults so operators can widen the budget for weaker
// providers (the #3356 DeepSeek case that needed more than 4 calls/turn and
// more than the default iteration budget).
func TestResolveEngineOptionsOverrides(t *testing.T) {
	t.Parallel()

	opts := askwiring.ResolveEngineOptions(envFunc(map[string]string{
		askwiring.EnvAskMaxIterations:       "12",
		askwiring.EnvAskMaxToolCallsPerTurn: "8",
	}), nil)

	if opts.MaxIterations != 12 {
		t.Errorf("MaxIterations = %d, want 12", opts.MaxIterations)
	}
	if opts.MaxToolCallsPerTurn != 8 {
		t.Errorf("MaxToolCallsPerTurn = %d, want 8", opts.MaxToolCallsPerTurn)
	}
}

// TestResolveEngineOptionsInvalidFallsBack proves that non-numeric, zero, or
// negative values are ignored and the default is used instead. An operator
// fat-fingering the knob must never silently disable the safety bound.
func TestResolveEngineOptionsInvalidFallsBack(t *testing.T) {
	t.Parallel()

	def := engine.DefaultOptions()
	cases := []struct {
		name string
		val  string
	}{
		{"non-numeric", "abc"},
		{"zero", "0"},
		{"negative", "-3"},
		{"empty", ""},
		{"whitespace", "   "},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := askwiring.ResolveEngineOptions(envFunc(map[string]string{
				askwiring.EnvAskMaxIterations:       tc.val,
				askwiring.EnvAskMaxToolCallsPerTurn: tc.val,
			}), nil)
			if opts.MaxIterations != def.MaxIterations {
				t.Errorf("MaxIterations = %d, want default %d for %q",
					opts.MaxIterations, def.MaxIterations, tc.val)
			}
			if opts.MaxToolCallsPerTurn != def.MaxToolCallsPerTurn {
				t.Errorf("MaxToolCallsPerTurn = %d, want default %d for %q",
					opts.MaxToolCallsPerTurn, def.MaxToolCallsPerTurn, tc.val)
			}
		})
	}
}

// TestResolveEngineOptionsClampsCeiling proves that an unreasonably large
// override is clamped to the documented ceiling so a misconfiguration cannot
// turn Ask into an unbounded provider-spend loop.
func TestResolveEngineOptionsClampsCeiling(t *testing.T) {
	t.Parallel()

	opts := askwiring.ResolveEngineOptions(envFunc(map[string]string{
		askwiring.EnvAskMaxIterations:       "100000",
		askwiring.EnvAskMaxToolCallsPerTurn: "100000",
	}), nil)

	if opts.MaxIterations != askwiring.MaxAskIterationsCeiling {
		t.Errorf("MaxIterations = %d, want clamp %d", opts.MaxIterations, askwiring.MaxAskIterationsCeiling)
	}
	if opts.MaxToolCallsPerTurn != askwiring.MaxAskToolCallsPerTurnCeiling {
		t.Errorf("MaxToolCallsPerTurn = %d, want clamp %d", opts.MaxToolCallsPerTurn, askwiring.MaxAskToolCallsPerTurnCeiling)
	}
}
