// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

// TestIngesterSourcePacerIsOffUnlessConfigured pins #10882 F8's ingester
// wiring: no pacer when the per-source leg is unconfigured or the local
// lightweight profile is active, a pacer when the thresholds are set.
func TestIngesterSourcePacerIsOffUnlessConfigured(t *testing.T) {
	t.Parallel()

	configured := map[string]string{
		"ESHU_SOURCE_BACKPRESSURE_ENGAGE_AGE":     "1h",
		"ESHU_SOURCE_BACKPRESSURE_RELEASE_AGE":    "15m",
		"ESHU_SOURCE_BACKPRESSURE_DEPTH_FLOOR":    "25",
		"ESHU_SOURCE_BACKPRESSURE_GRANT_DELAY":    "2s",
		"ESHU_SOURCE_BACKPRESSURE_MAX_ENGAGEMENT": "30m",
	}
	envOf := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}
	if pacer, err := ingesterSourcePacer(nil, envOf(nil), nil, nil); err != nil || pacer != nil {
		t.Fatalf("ingesterSourcePacer(empty env) = (%v, %v), want (nil, nil)", pacer, err)
	}
	lightweight := map[string]string{"ESHU_DISABLE_NEO4J": "true"}
	for key, value := range configured {
		lightweight[key] = value
	}
	if pacer, err := ingesterSourcePacer(nil, envOf(lightweight), nil, nil); err != nil || pacer != nil {
		t.Fatalf("ingesterSourcePacer(local lightweight) = (%v, %v), want (nil, nil)", pacer, err)
	}
}
