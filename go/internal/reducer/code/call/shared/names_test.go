// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import "testing"

// TestExactCandidateNamesDartReceiverFallback is the edge-case table for the
// Dart candidate branch in ExactCandidateNames: every qualified Dart
// full_name produces [qualified, bare-trailing-name] in that order,
// regardless of how codeCallDartQualifiedClassReceiver classifies the
// receiver segment (class vs. instance-variable vs. keyword vs. multi-segment
// vs. unrecognized) — Dart fails open and always appends the bare fallback.
func TestExactCandidateNamesDartReceiverFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fullName string
		want     []string
	}{
		{
			name:     "lowercase instance receiver",
			fullName: "repository.create",
			want:     []string{"repository.create", "create"},
		},
		{
			name:     "uppercase class/named-constructor receiver",
			fullName: "Point.origin",
			want:     []string{"Point.origin", "origin"},
		},
		{
			name:     "leading underscore, lowercase receiver after strip",
			fullName: "_repository.save",
			want:     []string{"_repository.save", "save"},
		},
		{
			name:     "leading underscore, uppercase receiver after strip",
			fullName: "_PrivateCache.instance",
			want:     []string{"_PrivateCache.instance", "instance"},
		},
		{
			name:     "keyword receiver",
			fullName: "super.dispose",
			want:     []string{"super.dispose", "dispose"},
		},
		{
			name:     "multi-segment receiver",
			fullName: "a.b.c",
			want:     []string{"a.b.c", "c"},
		},
		{
			name:     "no-alpha unrecognized receiver fails open",
			fullName: "_.create",
			want:     []string{"_.create", "create"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			call := map[string]any{
				"name":      TrailingName(tt.fullName),
				"full_name": tt.fullName,
			}
			got := ExactCandidateNames(call, "dart")
			if len(got) != len(tt.want) {
				t.Fatalf("ExactCandidateNames(%q) = %#v, want %#v", tt.fullName, got, tt.want)
			}
			for i, name := range tt.want {
				if got[i] != name {
					t.Fatalf("ExactCandidateNames(%q)[%d] = %q, want %q (full: %#v)", tt.fullName, i, got[i], name, got)
				}
			}
		})
	}
}

// TestCodeCallDartQualifiedClassReceiver documents and pins the
// classification ExactCandidateNames' doc comment relies on: an
// UpperCamelCase qualifier (after stripping a leading "_" or "$") is a
// class/static/named-constructor reference; everything else (lowercase,
// keyword, multi-segment, or unrecognized) is treated as an instance-variable
// receiver. The candidate list itself does not branch on this — see
// TestExactCandidateNamesDartReceiverFallback — but the classifier must
// still be independently correct.
func TestCodeCallDartQualifiedClassReceiver(t *testing.T) {
	t.Parallel()

	tests := []struct {
		fullName string
		want     bool
	}{
		{"repository.create", false},
		{"Point.origin", true},
		{"_repository.save", false},
		{"_PrivateCache.instance", true},
		{"super.dispose", false},
		{"this.dispose", false},
		{"a.b.c", false},
		{"_.create", false},
		{"create", false},
	}

	for _, tt := range tests {
		if got := codeCallDartQualifiedClassReceiver(tt.fullName); got != tt.want {
			t.Fatalf("codeCallDartQualifiedClassReceiver(%q) = %v, want %v", tt.fullName, got, tt.want)
		}
	}
}
