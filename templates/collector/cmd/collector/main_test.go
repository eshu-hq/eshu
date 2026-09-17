// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"strings"
	"testing"

	collector "github.com/eshu-hq/eshu-collector-template"
)

// TestNestedOptionalStringSeparatesMissingFromMalformed proves a missing key
// falls back to the placeholder while a present-but-malformed value fails
// closed instead of silently emitting against the placeholder.
func TestNestedOptionalStringSeparatesMissingFromMalformed(t *testing.T) {
	t.Parallel()

	value, present, err := nestedOptionalString(map[string]any{}, "source", "sourceURI")
	if err != nil || present || value != "" {
		t.Fatalf("missing key = (%q, %v, %v), want empty/absent/nil", value, present, err)
	}
	if _, _, err := nestedOptionalString(map[string]any{
		"source": map[string]any{"sourceURI": 42},
	}, "source", "sourceURI"); err == nil {
		t.Fatal("malformed value error = nil, want failure")
	}
	value, present, err = nestedOptionalString(map[string]any{
		"source": map[string]any{"sourceURI": "https://example.invalid/x"},
	}, "source", "sourceURI")
	if err != nil || !present || value != "https://example.invalid/x" {
		t.Fatalf("valid key = (%q, %v, %v), want value/present/nil", value, present, err)
	}
}

// TestNegativeLimitFlagsFailClosed proves negative CLI limits fail instead
// of silently selecting defaults, matching the config-file posture.
func TestNegativeLimitFlagsFailClosed(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := run([]string{"--max-records", "-5"}, strings.NewReader(""), &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "non-negative") {
		t.Fatalf("run(negative flag) error = %v, want non-negative rejection", err)
	}
}

// TestNestedLimitsConsumesConfigBlock proves the limits block from
// config.example.yaml reaches CollectOptions: absent selects defaults,
// partial overrides per field, malformed fails closed.
func TestNestedLimitsConsumesConfigBlock(t *testing.T) {
	t.Parallel()

	deflated, err := nestedLimits(map[string]any{})
	if err != nil {
		t.Fatalf("nestedLimits(empty) error = %v", err)
	}
	if deflated != collector.DefaultResourceUse() {
		t.Fatalf("nestedLimits(empty) = %+v, want defaults", deflated)
	}
	partial, err := nestedLimits(map[string]any{
		"limits": map[string]any{"maxRecordsPerClaim": 7.0},
	})
	if err != nil {
		t.Fatalf("nestedLimits(partial) error = %v", err)
	}
	if partial.MaxRecordsPerClaim != 7 || partial.MaxPayloadBytes != collector.DefaultResourceUse().MaxPayloadBytes {
		t.Fatalf("nestedLimits(partial) = %+v, want override + defaults", partial)
	}
	if _, err := nestedLimits(map[string]any{
		"limits": map[string]any{"maxRecordsPerClaim": "many"},
	}); err == nil {
		t.Fatal("nestedLimits(malformed) error = nil, want failure")
	}
}
