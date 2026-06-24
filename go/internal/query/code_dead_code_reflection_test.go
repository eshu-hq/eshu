// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

func TestBuildDeadCodeAnalysisForLanguageReportsReflectionModeledTruth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		language string
		want     bool
	}{
		{name: "java", language: "java", want: true},
		{name: "go", language: "go", want: false},
		{name: "unspecified", language: "", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			analysis := buildDeadCodeAnalysisForLanguage(nil, nil, deadCodePolicyStats{}, tt.language)
			if got := analysis["reflection_modeled"]; got != tt.want {
				t.Fatalf("analysis[reflection_modeled] = %#v, want %#v", got, tt.want)
			}
			languages, ok := analysis["reflection_modeled_languages"].([]string)
			if !ok {
				t.Fatalf("analysis[reflection_modeled_languages] type = %T, want []string", analysis["reflection_modeled_languages"])
			}
			if got, want := languages, []string{"java"}; !equalStringSlices(got, want) {
				t.Fatalf("analysis[reflection_modeled_languages] = %#v, want %#v", got, want)
			}
		})
	}
}
