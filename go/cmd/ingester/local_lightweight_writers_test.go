// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
)

// TestLocalLightweightProfileWritersAcceptAndCount pins the local lightweight
// profile's stand-ins: the canonical writer accepts a materialization, the
// reducer intent writer reports every intent as written, and the closer is a
// no-op. The profile has no graph or reducer queue behind it, so nothing may
// fail or be dropped silently from the caller's point of view.
func TestLocalLightweightProfileWritersAcceptAndCount(t *testing.T) {
	t.Parallel()

	lightweight := func(key string) string {
		if key == "ESHU_QUERY_PROFILE" {
			return "local_lightweight"
		}
		return ""
	}
	writer, closer, ok := maybeLocalLightweightCanonicalWriter(lightweight)
	if !ok || writer == nil || closer == nil {
		t.Fatalf("maybeLocalLightweightCanonicalWriter(lightweight) = (%v, %v, %v), want a writer and closer", writer, closer, ok)
	}
	if err := writer.Write(context.Background(), canonical.CanonicalMaterialization{}); err != nil {
		t.Fatalf("lightweight canonical Write() error = %v, want nil", err)
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("lightweight Close() error = %v, want nil", err)
	}
	if writer, closer, ok := maybeLocalLightweightCanonicalWriter(func(string) string { return "" }); ok || writer != nil || closer != nil {
		t.Fatalf("maybeLocalLightweightCanonicalWriter(default profile) = (%v, %v, %v), want none", writer, closer, ok)
	}

	intents := reducerIntentWriterForProfile(lightweight, nil)
	result, err := intents.Enqueue(context.Background(), []runtime.ReducerIntent{{}, {}, {}})
	if err != nil || result.Count != 3 {
		t.Fatalf("lightweight Enqueue() = (%+v, %v), want a count of 3", result, err)
	}
	// Under the lightweight profile the admission wrapper keeps that writer
	// unwrapped: there is no reducer queue to gate against.
	gated, err := ingesterAdmissionIntentWriter(nil, lightweight, nil, nil)
	if err != nil {
		t.Fatalf("ingesterAdmissionIntentWriter(lightweight) error = %v, want nil", err)
	}
	if result, err := gated.Enqueue(context.Background(), []runtime.ReducerIntent{{}}); err != nil || result.Count != 1 {
		t.Fatalf("admission writer under lightweight = (%+v, %v), want the unwrapped writer's count of 1", result, err)
	}
}
