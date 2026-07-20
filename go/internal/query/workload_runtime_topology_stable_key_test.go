// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

// unmarshalableProperties returns a properties map that json.Marshal cannot
// encode, exercising the marshal-error branch of stablePropertiesKey.
func unmarshalableProperties() map[string]any {
	return map[string]any{"bad": make(chan int)}
}

// TestStablePropertiesKeyMarshalErrorFailsClosed proves invalid relationship
// properties cannot silently participate in deterministic edge selection.
func TestStablePropertiesKeyMarshalErrorFailsClosed(t *testing.T) {
	got, err := stablePropertiesKey(unmarshalableProperties())
	if err == nil {
		t.Fatal("stablePropertiesKey() error = nil, want marshal failure")
	}
	if got != "" {
		t.Fatalf("stablePropertiesKey() = %q on error, want empty key", got)
	}
	if !strings.Contains(err.Error(), "marshal graph relationship properties") {
		t.Fatalf("stablePropertiesKey() error = %q, want relationship-property context", err)
	}
}

// TestStablePropertiesKeyValidInputsAreDeterministic proves valid property
// sets retain canonical JSON ordering for stable edge tie-breaking.
func TestStablePropertiesKeyValidInputsAreDeterministic(t *testing.T) {
	tests := []struct {
		name       string
		properties map[string]any
		want       string
	}{
		{name: "nil", properties: nil, want: "null"},
		{name: "empty", properties: map[string]any{}, want: "{}"},
		{
			name: "sorted keys",
			properties: map[string]any{
				"reason":     "helm",
				"confidence": 0.99,
			},
			want: `{"confidence":0.99,"reason":"helm"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := stablePropertiesKey(test.properties)
			if err != nil {
				t.Fatalf("stablePropertiesKey() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("stablePropertiesKey() = %q, want %q", got, test.want)
			}
		})
	}
}
