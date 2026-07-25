// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"strings"
	"testing"
)

// TestPayloadStringFieldReturnsValue proves the happy path unwraps a string
// value unchanged.
func TestPayloadStringFieldReturnsValue(t *testing.T) {
	t.Parallel()

	got, err := payloadStringField(map[string]any{"run_id": "4200"}, "run_id")
	if err != nil {
		t.Fatalf("payloadStringField() error = %v, want nil", err)
	}
	if want := "4200"; got != want {
		t.Fatalf("payloadStringField() = %q, want %q", got, want)
	}
}

// TestPayloadStringFieldRejectsWrongType proves a non-string value (e.g. a
// future gitlabSharedPayload refactor that starts storing run_id as an int or
// json.Number) returns a clear typed error instead of the caller panicking on
// an unchecked type assertion.
func TestPayloadStringFieldRejectsWrongType(t *testing.T) {
	t.Parallel()

	_, err := payloadStringField(map[string]any{"run_attempt": 1}, "run_attempt")
	if err == nil {
		t.Fatal("payloadStringField() error = nil, want type mismatch error")
	}
	if !strings.Contains(err.Error(), "run_attempt") || !strings.Contains(err.Error(), "int") {
		t.Fatalf("payloadStringField() error = %q, want it to name the field and the wrong Go type", err)
	}
}

// TestPayloadStringFieldRejectsMissingKey proves a missing key (nil map
// value) also returns an error rather than panicking, matching the
// wrong-type case (a nil interface fails the .(string) assertion the same
// way an int does).
func TestPayloadStringFieldRejectsMissingKey(t *testing.T) {
	t.Parallel()

	_, err := payloadStringField(map[string]any{}, "run_id")
	if err == nil {
		t.Fatal("payloadStringField() error = nil, want missing-key error")
	}
}
