// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membership

import (
	"strings"
	"testing"
	"time"
)

func TestParseLivenessWindow(t *testing.T) {
	t.Parallel()

	accepted := map[string]time.Duration{
		"":       48 * time.Hour,
		"  ":     48 * time.Hour,
		"48h":    48 * time.Hour,
		"1h":     time.Hour,
		" 72h ":  72 * time.Hour,
		"90m":    90 * time.Minute,
		"1h0m1s": time.Hour + time.Second,
	}
	for raw, want := range accepted {
		got, err := ParseLivenessWindow(raw)
		if err != nil || got != want {
			t.Fatalf("ParseLivenessWindow(%q) = %v, %v; want %v", raw, got, err, want)
		}
	}

	for _, raw := range []string{"59m", "59m59s", "0", "-48h", "garbage", "48", "600000000h"} {
		got, err := ParseLivenessWindow(raw)
		if err == nil {
			t.Fatalf("ParseLivenessWindow(%q) = %v, nil; want an error", raw, got)
		}
		if !strings.Contains(err.Error(), LivenessWindowEnv) {
			t.Fatalf("ParseLivenessWindow(%q) error = %v, want it to name %s", raw, err, LivenessWindowEnv)
		}
	}
}
