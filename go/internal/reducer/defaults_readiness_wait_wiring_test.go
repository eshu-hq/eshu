// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/reducer/iamcan"
)

type noopIAMCanPerformWriter struct{}

func (noopIAMCanPerformWriter) WriteIAMCanPerformEdges(context.Context, []map[string]any, string, string, string) error {
	return nil
}

func (noopIAMCanPerformWriter) RetractIAMCanPerformEdges(context.Context, []string, string, string) error {
	return nil
}

// TestDefaultHandlersWireReadinessWaitLedger proves the one ledger in
// DefaultHandlers reaches both #6785 cross-scope handlers. Without it the
// handlers fall back to the per-row anchor that supersession resets (R2-F1).
func TestDefaultHandlersWireReadinessWaitLedger(t *testing.T) {
	t.Parallel()
	ledger := &usesWaitLedger{rows: map[string]crossscope.ReadinessWait{}}
	handlers := DefaultHandlers{
		FactLoader:                          &stubFactLoader{},
		IAMCanPerformEdgeWriter:             noopIAMCanPerformWriter{},
		WorkloadCloudRelationshipEdgeWriter: &recordingWorkloadCloudRelationshipWriter{},
		CrossScopeHandlers:                  CrossScopeHandlers{ReadinessWaits: ledger},
	}
	var sawCanPerform, sawUses bool
	for _, definition := range appendCloudPostureEdgeAdditiveDomains(nil, handlers) {
		if handler, ok := definition.Handler.(iamcan.IAMCanPerformMaterializationHandler); ok {
			sawCanPerform = handler.ReadinessWaits == crossscope.ReadinessWaitLedger(ledger)
		}
	}
	for _, definition := range appendCloudRelationshipAdditiveDomains(nil, handlers) {
		if handler, ok := definition.Handler.(WorkloadCloudRelationshipMaterializationHandler); ok {
			sawUses = handler.ReadinessWaits == crossscope.ReadinessWaitLedger(ledger)
		}
	}
	if !sawCanPerform || !sawUses {
		t.Fatalf("ledger wired: CAN_PERFORM=%v USES=%v, want both", sawCanPerform, sawUses)
	}
}
