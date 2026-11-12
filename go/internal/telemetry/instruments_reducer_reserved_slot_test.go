// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
)

// TestReservedSlotInstrumentsRegistered pins the #10581 reserved-slot split
// registration: one counter of batch-claim reserved search-doc slot outcomes
// by closed outcome.
func TestReservedSlotInstrumentsRegistered(t *testing.T) {
	t.Parallel()

	inst, err := NewInstruments(noop.NewMeterProvider().Meter("reserved-slot-test"))
	require.NoError(t, err)
	assert.NotNil(t, inst.ReservedSlotClaims, "ReservedSlotClaims counter should be registered")
}

// TestReservedSlotClaimOutcomesClosed pins the closed outcome set the
// reserved-slot counter labels. The storage claim path records exactly these
// outcomes; adding an outcome here without an emission site (or vice versa)
// must fail loudly.
func TestReservedSlotClaimOutcomesClosed(t *testing.T) {
	t.Parallel()

	want := []string{
		"reserved_used",
		"fallback",
	}
	if got := ReservedSlotClaimOutcomes(); !slices.Equal(got, want) {
		t.Fatalf("ReservedSlotClaimOutcomes() = %v, want %v", got, want)
	}
}
