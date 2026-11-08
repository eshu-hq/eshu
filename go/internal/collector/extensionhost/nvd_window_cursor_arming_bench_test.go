// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// BenchmarkCollectedGenerationArmingCost guards the #9877 Path-A arming
// cost on the commit hot path: a non-window generation must stay at one
// prefix comparison, and a window generation at one snapshot scan.
func BenchmarkCollectedGenerationArmingCost(b *testing.B) {
	source := mustNewSourceWithManifest(b, testManifest(), &recordingRunner{}, nil)
	item := testWorkItem()
	result := completeResult(item, testSDKFact(item))
	window := testWorkItem()
	window.ScopeID = testNVDWindowScopeForArming
	windowResult := completeResult(window, testSDKFact(window))
	verdict := []facts.Envelope{testNVDWindowSnapshotEnvelope(testNVDWindowVerdictPayload(true))}

	b.Run("non_window_scope", func(b *testing.B) {
		envs := []facts.Envelope{testNVDWindowSnapshotEnvelope(testNVDWindowVerdictPayload(true))}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			collected := source.collectedGeneration(item, result, envs, "")
			if collected.StateWriter != nil {
				b.Fatal("StateWriter armed for a non-window scope")
			}
		}
	})
	b.Run("window_scope_advancing", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			collected := source.collectedGeneration(window, windowResult, verdict, "")
			if collected.StateWriter == nil {
				b.Fatal("StateWriter not armed for an advancing window verdict")
			}
		}
	})
}
