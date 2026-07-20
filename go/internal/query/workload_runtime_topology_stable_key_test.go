// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// unmarshalableProperties returns a properties map that json.Marshal cannot
// encode, exercising the marshal-error branch of stablePropertiesKey.
func unmarshalableProperties() map[string]any {
	return map[string]any{"bad": make(chan int)}
}

// TestStablePropertiesKeyMarshalErrorReturnsSentinel proves an unmarshalable
// property set returns the distinct sentinel (never the empty string that would
// collide with a real empty-properties key) and logs via the provided logger.
func TestStablePropertiesKeyMarshalErrorReturnsSentinel(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	got := stablePropertiesKey(logger, unmarshalableProperties())

	if got != stablePropertiesKeyMarshalErrorSentinel {
		t.Fatalf("stablePropertiesKey() = %q, want sentinel %q", got, stablePropertiesKeyMarshalErrorSentinel)
	}
	if got == "" {
		t.Fatal("stablePropertiesKey() returned empty string, which collides with a real empty-properties key")
	}
	if buf.Len() == 0 {
		t.Fatal("stablePropertiesKey() did not log the marshal error")
	}
}

// TestStablePropertiesKeyMarshalErrorNilLoggerSafe proves the marshal-error
// branch never panics when no logger is threaded through the call site.
func TestStablePropertiesKeyMarshalErrorNilLoggerSafe(t *testing.T) {
	got := stablePropertiesKey(nil, unmarshalableProperties())

	if got != stablePropertiesKeyMarshalErrorSentinel {
		t.Fatalf("stablePropertiesKey(nil, ...) = %q, want sentinel %q", got, stablePropertiesKeyMarshalErrorSentinel)
	}
}

// TestStablePropertiesKeySentinelCannotCollide proves the sentinel is distinct
// from every real JSON key stablePropertiesKey emits for valid input, including
// the empty and nil property sets, and sorts after them so a marshal-error edge
// never wins the deterministic "smallest key" tie-break over real evidence.
func TestStablePropertiesKeySentinelCannotCollide(t *testing.T) {
	realKeys := []string{
		stablePropertiesKey(nil, nil),
		stablePropertiesKey(nil, map[string]any{}),
		stablePropertiesKey(nil, map[string]any{"confidence": 0.99, "reason": "helm"}),
	}
	for _, key := range realKeys {
		if key == stablePropertiesKeyMarshalErrorSentinel {
			t.Fatalf("real key %q collides with the marshal-error sentinel", key)
		}
		if key >= stablePropertiesKeyMarshalErrorSentinel {
			t.Fatalf("real key %q does not sort before the sentinel; sentinel could win a tie-break", key)
		}
	}
	if !strings.HasPrefix(stablePropertiesKeyMarshalErrorSentinel, "￿") {
		t.Fatalf("sentinel %q must start with U+FFFF so it cannot equal real JSON", stablePropertiesKeyMarshalErrorSentinel)
	}
}
