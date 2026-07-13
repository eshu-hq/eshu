// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"encoding/json"
	"strings"
	"testing"
)

var benchmarkMarshaledPayload []byte

func TestMarshalPayloadSanitizesForPostgresJSONB(t *testing.T) {
	t.Parallel()

	t.Run("strips null unicode escapes", func(t *testing.T) {
		t.Parallel()
		payload := map[string]any{"content": "hello\u0000world"}
		data, err := marshalPayload(payload)
		if err != nil {
			t.Fatalf("marshalPayload() error = %v, want nil", err)
		}
		if strings.Contains(string(data), `\u0000`) {
			t.Fatalf("output contains \\u0000: %s", data)
		}
		if !strings.Contains(string(data), "hello") {
			t.Fatalf("missing content: %s", data)
		}
	})

	t.Run("preserves literal null escape source text", func(t *testing.T) {
		t.Parallel()
		const sourceText = `const delimiter = "\u0000"`
		payload := map[string]any{"content": sourceText}

		data, err := marshalPayload(payload)
		if err != nil {
			t.Fatalf("marshalPayload() error = %v, want nil", err)
		}

		var decoded map[string]any
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("json.Unmarshal() error = %v for %s", err, data)
		}
		if got := decoded["content"]; got != sourceText {
			t.Fatalf("decoded content = %#v, want %#v", got, sourceText)
		}
	})

	t.Run("distinguishes literal escape from adjacent null byte", func(t *testing.T) {
		t.Parallel()
		const sourceText = `literal \u0000 stays`
		payload := map[string]any{"content": sourceText + "\u0000removed"}

		data, err := marshalPayload(payload)
		if err != nil {
			t.Fatalf("marshalPayload() error = %v, want nil", err)
		}

		var decoded map[string]any
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("json.Unmarshal() error = %v for %s", err, data)
		}
		if got, want := decoded["content"], sourceText+"removed"; got != want {
			t.Fatalf("decoded content = %#v, want %#v", got, want)
		}
	})

	t.Run("strips null byte after literal backslash", func(t *testing.T) {
		t.Parallel()
		const sourcePrefix = `literal backslash \`
		payload := map[string]any{"content": sourcePrefix + "\u0000removed"}

		data, err := marshalPayload(payload)
		if err != nil {
			t.Fatalf("marshalPayload() error = %v, want nil", err)
		}

		var decoded map[string]any
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("json.Unmarshal() error = %v for %s", err, data)
		}
		if got, want := decoded["content"], sourcePrefix+"removed"; got != want {
			t.Fatalf("decoded content = %#v, want %#v", got, want)
		}
	})

	t.Run("strips raw control bytes", func(t *testing.T) {
		t.Parallel()
		payload := map[string]any{"content": "before\x01\x02\x03after"}
		data, err := marshalPayload(payload)
		if err != nil {
			t.Fatalf("marshalPayload() error = %v, want nil", err)
		}
		for _, b := range data {
			if b < 0x20 && b != '\t' && b != '\n' && b != '\r' {
				t.Fatalf("output contains raw control byte 0x%02x: %s", b, data)
			}
		}
	})

	t.Run("clean payload passes through unchanged", func(t *testing.T) {
		t.Parallel()
		payload := map[string]any{"name": "eshu"}
		data, err := marshalPayload(payload)
		if err != nil {
			t.Fatalf("marshalPayload() error = %v, want nil", err)
		}
		if !strings.Contains(string(data), "eshu") {
			t.Fatalf("missing content: %s", data)
		}
	})

	t.Run("empty payload returns empty object", func(t *testing.T) {
		t.Parallel()
		data, err := marshalPayload(nil)
		if err != nil {
			t.Fatalf("marshalPayload() error = %v, want nil", err)
		}
		if string(data) != "{}" {
			t.Fatalf("got %s, want {}", data)
		}
	})
}

func BenchmarkMarshalPayloadSourceText(b *testing.B) {
	testCases := map[string]string{
		"clean":        strings.Repeat("const delimiter = colon\n", 256),
		"literal_null": strings.Repeat(`const delimiter = "\u0000"`+"\n", 256),
	}
	for name, sourceText := range testCases {
		b.Run(name, func(b *testing.B) {
			payload := map[string]any{"content": sourceText}
			b.ReportAllocs()
			for b.Loop() {
				data, err := marshalPayload(payload)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkMarshaledPayload = data
			}
		})
	}
}
