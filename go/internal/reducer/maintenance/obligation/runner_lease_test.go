// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package obligation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestDefaultActivationLeaseIsThirtyMinutes pins the arbiter-ruled #7638
// interim default: one claim holds an obligation for 30 minutes so the
// measured 395 s full-corpus (984-scope) pass fits inside the
// lease-minus-a-fifth deadline. Lower back only when a rewrite re-measures
// the pass p99 far below the deadline.
func TestDefaultActivationLeaseIsThirtyMinutes(t *testing.T) {
	t.Parallel()
	require.Equal(t, 30*time.Minute, DefaultActivationLease)
	require.Equal(t, 30*time.Minute, Config{Owner: "owner"}.withDefaults().Lease)
}

// TestMaintenanceDeadlineStaysLeaseMinusAFifth pins the unchanged deadline
// rule: the maintenance callback is cancelled a fifth of the lease before
// the lease ends, whatever the default lease is.
func TestMaintenanceDeadlineStaysLeaseMinusAFifth(t *testing.T) {
	t.Parallel()
	leaseUntil := time.Now().Add(30 * time.Minute)
	ctx, cancel := maintenanceContext(t.Context(), leaseUntil, 30*time.Minute)
	defer cancel()
	deadline, ok := ctx.Deadline()
	require.True(t, ok, "maintenance context must carry a deadline")
	require.WithinDuration(t, leaseUntil.Add(-(30*time.Minute)/5), deadline, time.Second)
}
