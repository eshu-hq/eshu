// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rdsposture

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
)

// Local copies of the reducer-root test helpers this family's tests used
// before the move (issue #6061). Go test files cannot share unexported
// symbols across a package boundary, so they are duplicated here verbatim
// rather than exported from the root for test-only use.

// stubFactLoader replays a fixed envelope batch and counts loads.
type stubFactLoader struct {
	envelopes []facts.Envelope
	calls     int
}

func (f *stubFactLoader) ListFacts(_ context.Context, _, _ string) ([]facts.Envelope, error) {
	f.calls++
	return f.envelopes, nil
}

// readyLookup returns a ReadinessLookup that always answers (ready, found).
func readyLookup(ready, found bool) gpphase.ReadinessLookup {
	return func(_ gpphase.PhaseKey, _ gpphase.Phase) (bool, bool) {
		return ready, found
	}
}
