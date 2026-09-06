// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ec2blockkms

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Local copy of the reducer-root test helper this family's tests used before
// the move (issue #6061). Go test files cannot share unexported symbols across
// a package boundary, so it is duplicated here verbatim rather than exported
// from the root for test-only use.

// stubFactLoader replays a fixed envelope batch and counts loads.
type stubFactLoader struct {
	envelopes []facts.Envelope
	calls     int
}

func (f *stubFactLoader) ListFacts(_ context.Context, _, _ string) ([]facts.Envelope, error) {
	f.calls++
	return f.envelopes, nil
}
