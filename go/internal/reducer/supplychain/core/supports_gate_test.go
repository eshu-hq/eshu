// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"testing"
)

// gatedFanOut is a fan-out stub whose supports gate answers from gateErr.
type gatedFanOut struct {
	*stubFanOut
	gateErr   error
	gateCalls int
}

func (g *gatedFanOut) RequireSupportsReady(context.Context) error {
	g.gateCalls++
	return g.gateErr
}

// TestHandlerDefersBeforeAnyWorkWhileSupportsAreNotReady pins the #11197 handler
// entry gate (arbiter-b2c decision 3): a supports table that is not ready defers
// the pass before it loads evidence, issues a fencing token, writes findings or
// reads a holder, and the refusal reaches the queue with its non-counting class.
// The GREEN half runs the same pass against a ready table and requires the
// writer to run, so the gate cannot pass by refusing everything.
func TestHandlerDefersBeforeAnyWorkWhileSupportsAreNotReady(t *testing.T) {
	t.Parallel()

	t.Run("not ready defers before any load or write", func(t *testing.T) {
		t.Parallel()
		port := &gatedFanOut{
			stubFanOut: &stubFanOut{holders: []string{"holder"}},
			gateErr:    &SupplyChainImpactSupportsNotReadyError{Reader: "fanout_holders"},
		}
		writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
		handler := fanOutHandler(port, writer, nil)
		issuer := newTestImpactFencingTokenIssuer()
		handler.FencingTokenIssuer = issuer
		loader := handler.FactLoader.(*stubSupplyChainImpactFactLoader)

		_, err := handler.Handle(context.Background(), fanOutIntent(1, nil))
		var classed interface{ FailureClass() string }
		if !errors.As(err, &classed) || classed.FailureClass() != SupplyChainImpactSupportsNotReadyFailureClass {
			t.Fatalf("error = %v, want the %q class", err, SupplyChainImpactSupportsNotReadyFailureClass)
		}
		if port.gateCalls != 1 {
			t.Fatalf("gate calls = %d, want 1", port.gateCalls)
		}
		if len(writer.writes) != 0 {
			t.Fatalf("writer calls = %d, want 0: the pass wrote before the gate", len(writer.writes))
		}
		if len(port.findCalls) != 0 || len(port.reopened) != 0 {
			t.Fatalf("holder reads %v / reopens %v, want none", port.findCalls, port.reopened)
		}
		if len(loader.filters) != 0 || len(loader.kindCalls) != 0 || len(loader.activeCalls) != 0 {
			t.Fatal("the pass loaded evidence before the gate")
		}
		if issuer.issued.Load() != 0 {
			t.Fatalf("fencing tokens issued = %d, want 0", issuer.issued.Load())
		}
	})

	t.Run("ready runs the pass", func(t *testing.T) {
		t.Parallel()
		port := &gatedFanOut{stubFanOut: &stubFanOut{holders: []string{"holder"}}}
		writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
		if _, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if port.gateCalls != 1 || len(writer.writes) != 1 {
			t.Fatalf("gate calls = %d, writer calls = %d, want 1 and 1", port.gateCalls, len(writer.writes))
		}
	})

	t.Run("a probe failure keeps its own error", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("probe failed")
		port := &gatedFanOut{stubFanOut: &stubFanOut{}, gateErr: boom}
		writer := &changeWriter{}
		_, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, nil))
		if !errors.Is(err, boom) || len(writer.writes) != 0 {
			t.Fatalf("error = %v, writer calls = %d, want the probe error and no write", err, len(writer.writes))
		}
	})
}
