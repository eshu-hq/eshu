// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"slices"
	"time"
)

// stubInFlightClaimTime is the claim identity of an in-flight stub scope that
// stubFanOut.claimedAt does not name.
var stubInFlightClaimTime = time.Date(2026, 11, 24, 1, 0, 0, 0, time.UTC)

// inFlightClaimsLocked returns the non-marker in-flight scopes as claims.
// Callers hold s.mu.
func (s *stubFanOut) inFlightClaimsLocked() []InFlightClaim {
	var out []InFlightClaim
	for _, scope := range s.inFlight {
		if slices.Contains(s.inFlightMarkers, scope) {
			continue
		}
		at, ok := s.claimedAt[scope]
		if !ok {
			at = stubInFlightClaimTime
		}
		out = append(out, InFlightClaim{ScopeID: scope, ClaimedAt: at})
	}
	return out
}

// FindInFlightNonMarkerClaims is set B: it records the same "in-flight" read
// and honors the same inFlightErr as FindInFlightNonMarkerScopes.
func (s *stubFanOut) FindInFlightNonMarkerClaims(_ context.Context, _ string) ([]InFlightClaim, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inFlightCalls++
	s.reads = append(s.reads, "in-flight")
	if s.inFlightErr != nil {
		return nil, s.inFlightErr
	}
	return s.inFlightClaimsLocked(), nil
}

// SnapshotInFlightNonMarkerClaims is set A, the load-end read. It is not
// recorded in s.reads, so the fan-out read-order assertions stay about the
// fan-out's own two reads.
func (s *stubFanOut) SnapshotInFlightNonMarkerClaims(_ context.Context, _ string) ([]InFlightClaim, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.earlyCalls++
	if s.earlyErr != nil {
		return nil, s.earlyErr
	}
	if s.earlySet {
		return append([]InFlightClaim(nil), s.early...), nil
	}
	return s.inFlightClaimsLocked(), nil
}
