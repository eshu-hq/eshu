// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package decode

import "testing"

type rawTextStringer string

func (value rawTextStringer) String() string { return string(value) }

func TestPayloadRawTextPreservesStringShapedScalars(t *testing.T) {
	t.Parallel()
	if got, ok := PayloadString(map[string]any{"body": "  metadata \n"}, "body"); !ok || got != "metadata" {
		t.Fatalf("PayloadString must retain trimmed metadata semantics: %q, %t", got, ok)
	}
	for _, value := range []string{"  a\n", " \t\n ", ""} {
		got, ok := PayloadRawText(map[string]any{"body": value}, "body")
		if !ok || got != value {
			t.Fatalf("PayloadRawText(%q) = %q, %t", value, got, ok)
		}
	}
	if got, ok := PayloadRawText(map[string]any{"body": rawTextStringer("  stringer\n")}, "body"); !ok || got != "  stringer\n" {
		t.Fatalf("PayloadRawText stringer = %q, %t", got, ok)
	}
	for _, payload := range []map[string]any{nil, {}, {"body": nil}, {"body": 123}, {"body": true}, {"body": []byte("raw")}} {
		if got, ok := PayloadRawText(payload, "body"); ok || got != "" {
			t.Fatalf("PayloadRawText(%#v) = %q, %t; want absent", payload, got, ok)
		}
	}
}
