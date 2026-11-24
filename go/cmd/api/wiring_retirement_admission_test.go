// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/query/admin/retirement"
)

// fakeRetirementTrigger is a no-store trigger port for the wiring test:
// triggering succeeds without opening tombstone rows or fencing anything.
type fakeRetirementTrigger struct{}

func (f *fakeRetirementTrigger) TriggerRetirement(
	_ context.Context,
	_ retirement.TriggerRequest,
) (retirement.TriggerResult, error) {
	return retirement.TriggerResult{}, nil
}

// TestNewRetirementHandlerTakesAdmissionContract proves the #8872 seam:
// the retirement handler is built with the versioned retirement trigger
// admission contract, not a direct trigger handle. The handler must carry
// the exact injected port (identity, not merely non-nil) so eshu-api can
// later bind a core-issued trigger endpoint.
func TestNewRetirementHandlerTakesAdmissionContract(t *testing.T) {
	t.Parallel()

	triggerer := &fakeRetirementTrigger{}
	handler := newRetirementHandler(nil, triggerer, nil)
	if handler == nil {
		t.Fatal("newRetirementHandler() = nil, want a handler bound to the admission contract")
	}
	if handler.Triggerer != query.RetirementAdmission(triggerer) {
		t.Fatalf("handler.Triggerer = %T(%v), want the exact injected admission contract %T(%v)", handler.Triggerer, handler.Triggerer, triggerer, triggerer)
	}
}
