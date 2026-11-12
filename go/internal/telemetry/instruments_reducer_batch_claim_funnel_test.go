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

// TestBatchClaimFunnelInstrumentsRegistered pins the #10565 box-6 funnel
// registration: candidates in, representatives after the fence, and drops
// by closed reason.
func TestBatchClaimFunnelInstrumentsRegistered(t *testing.T) {
	t.Parallel()

	inst, err := NewInstruments(noop.NewMeterProvider().Meter("batch-claim-funnel-test"))
	require.NoError(t, err)
	assert.NotNil(t, inst.BatchClaimCandidates, "BatchClaimCandidates histogram should be registered")
	assert.NotNil(t, inst.BatchClaimRepresentatives, "BatchClaimRepresentatives histogram should be registered")
	assert.NotNil(t, inst.BatchClaimDrops, "BatchClaimDrops counter should be registered")
}

// TestBatchClaimDropReasonsClosed pins the closed drop-reason set the funnel
// counter labels. The storage funnel records exactly these reasons in fence
// order; adding a reason here without a funnel stage (or vice versa) must
// fail loudly.
func TestBatchClaimDropReasonsClosed(t *testing.T) {
	t.Parallel()

	want := []string{
		"not_ready",
		"not_representative",
		"projector_drain",
		"semantic_cap",
		"live_lease_fence",
		"over_limit",
		"lock_lost",
	}
	if got := BatchClaimDropReasons(); !slices.Equal(got, want) {
		t.Fatalf("BatchClaimDropReasons() = %v, want %v", got, want)
	}
}
