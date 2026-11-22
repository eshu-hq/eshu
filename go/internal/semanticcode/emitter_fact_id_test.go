// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticcode

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestEmitFactIDIsScopedButStableKeyIsNot is the #11203 regression: the same
// hint emitted under another scope or generation must get a different FactID
// (fact_records is keyed on fact_id alone), while StableFactKey stays the
// semantic identity shared across them.
func TestEmitFactIDIsScopedButStableKeyIsNot(t *testing.T) {
	t.Parallel()

	emitter, err := NewEmitter(testConfig())
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}
	emitOne := func(span CodeSpanInput) facts.Envelope {
		t.Helper()
		out, err := emitter.Emit(context.Background(), span, []HintInput{testHint()})
		if err != nil || len(out) != 1 {
			t.Fatalf("Emit() = %d envelopes, error %v, want 1, nil", len(out), err)
		}
		return out[0]
	}
	base := emitOne(testSpan())
	otherScope := testSpan()
	otherScope.ScopeID = "git:repository:billing-api"
	otherGeneration := testSpan()
	otherGeneration.GenerationID = "gen-2"
	for name, span := range map[string]CodeSpanInput{"scope": otherScope, "generation": otherGeneration} {
		got := emitOne(span)
		if got.FactID == base.FactID {
			t.Errorf("FactID is identical across another %s: %q", name, got.FactID)
		}
		if got.StableFactKey != base.StableFactKey {
			t.Errorf("StableFactKey changed across another %s: %q vs %q", name, got.StableFactKey, base.StableFactKey)
		}
	}
}
