// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package resultshape

import (
	"context"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]Mode{
		"": Dual, "dual": Dual, "DUAL": Dual, "structured": Structured, "Structured": Structured,
	} {
		if got, err := Parse(raw); err != nil || got != want {
			t.Fatalf("Parse(%q) = (%q, %v), want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{"text", "both", " structured", "structured "} {
		if _, err := Parse(raw); err == nil || !strings.Contains(err.Error(), "dual") || !strings.Contains(err.Error(), "structured") {
			t.Fatalf("Parse(%q) error = %v, want one naming dual and structured", raw, err)
		}
	}
}

func TestOrDual(t *testing.T) {
	t.Parallel()
	if OrDual("") != Dual || OrDual(Structured) != Structured || OrDual(Dual) != Dual {
		t.Fatal("OrDual must map only the zero value to Dual")
	}
}

func TestContextCarrier(t *testing.T) {
	t.Parallel()
	if _, ok := FromContext(context.Background()); ok {
		t.Fatal("empty context reported a shape")
	}
	if got, ok := FromContext(WithOverride(context.Background(), Structured)); !ok || got != Structured {
		t.Fatalf("FromContext = (%q, %v), want structured", got, ok)
	}
}
