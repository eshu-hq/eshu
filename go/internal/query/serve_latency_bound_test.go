// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"
	"time"
)

// satelliteRelayBound is the satellite's per-call relay deadline: 30 s in
// both spellings at satellite pin 08ebde4 (api/dispatch.go dispatchTimeout
// and internal/readclient DefaultTimeout, pinned equal there by
// TestDispatch_TimeoutMatchesReadContract).
const satelliteRelayBound = 30 * time.Second

// TestCoreReadinessProbeStaysFarBelowRelayBound pins the D4 latency
// budget from the core side: the /readyz probe must answer well before a
// satellite relay worker would time out, so a slow backend surfaces as
// the core's own 503 rather than holding a relay worker past the bound.
func TestCoreReadinessProbeStaysFarBelowRelayBound(t *testing.T) {
	t.Parallel()
	if coreReadinessProbeTimeout >= satelliteRelayBound {
		t.Fatalf("coreReadinessProbeTimeout = %v, want below the satellite relay bound %v",
			coreReadinessProbeTimeout, satelliteRelayBound)
	}
	if coreReadinessProbeTimeout > satelliteRelayBound/10 {
		t.Fatalf("coreReadinessProbeTimeout = %v, want an order of magnitude below the relay bound %v",
			coreReadinessProbeTimeout, satelliteRelayBound)
	}
}
