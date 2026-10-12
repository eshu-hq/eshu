// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import "testing"

func int64p(v int64) *int64 { return &v }
func intp(v int) *int       { return &v }

// TestClassifyNamesEveryFallbackReason pins the reader's trust condition
// (protocol section 4): stored rows are served only when the state row's
// content_version equals the current version, its extractor_version equals
// the reader's, and a derive has succeeded (row_count not NULL). Every other
// repository is served live, and the reason names why.
func TestClassifyNamesEveryFallbackReason(t *testing.T) {
	t.Parallel()

	const extractor = 3
	trusted := State{ContentVersion: int64p(7), ExtractorVersion: extractor, RowCount: intp(21)}
	tests := []struct {
		name    string
		state   *State
		version *int64
		want    Reason
	}{
		{"trusted", &trusted, int64p(7), ReasonTrusted},
		{"derived with no artifacts is trusted", &State{ContentVersion: int64p(7), ExtractorVersion: extractor, RowCount: intp(0)}, int64p(7), ReasonTrusted},
		{"trusted after a later failed derive of the same content", &State{ContentVersion: int64p(7), ExtractorVersion: extractor, RowCount: intp(21), FailureCount: 2}, int64p(7), ReasonTrusted},
		{"no state row", nil, int64p(7), ReasonMissing},
		{"no version row", &trusted, nil, ReasonMissing},
		{"neither row", nil, nil, ReasonMissing},
		{"no successful derive", &State{ExtractorVersion: extractor, FailureCount: 1}, int64p(7), ReasonFailed},
		{"content moved", &State{ContentVersion: int64p(6), ExtractorVersion: extractor, RowCount: intp(21)}, int64p(7), ReasonStaleVersion},
		{"older extractor", &State{ContentVersion: int64p(7), ExtractorVersion: extractor - 1, RowCount: intp(21)}, int64p(7), ReasonStaleExtractor},
		{"newer extractor", &State{ContentVersion: int64p(7), ExtractorVersion: extractor + 1, RowCount: intp(21)}, int64p(7), ReasonStaleExtractor},
		{"content moved and older extractor", &State{ContentVersion: int64p(6), ExtractorVersion: extractor - 1, RowCount: intp(21)}, int64p(7), ReasonStaleVersion},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Classify(tt.state, tt.version, extractor); got != tt.want {
				t.Fatalf("Classify() = %q, want %q", got, tt.want)
			}
		})
	}
}
