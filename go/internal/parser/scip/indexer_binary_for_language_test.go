// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scip

import "testing"

func TestBinaryForLanguage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		language   string
		wantBinary string
		wantOK     bool
	}{
		{name: "c", language: "c", wantBinary: "scip-clang", wantOK: true},
		{name: "cpp", language: "cpp", wantBinary: "scip-clang", wantOK: true},
		{name: "go", language: "go", wantBinary: "scip-go", wantOK: true},
		{name: "java", language: "java", wantBinary: "scip-java", wantOK: true},
		{name: "javascript", language: "javascript", wantBinary: "scip-typescript", wantOK: true},
		{name: "python", language: "python", wantBinary: "scip-python", wantOK: true},
		{name: "rust", language: "rust", wantBinary: "scip-rust", wantOK: true},
		{name: "typescript", language: "typescript", wantBinary: "scip-typescript", wantOK: true},
		{name: "upper case is normalized", language: "GO", wantBinary: "scip-go", wantOK: true},
		{name: "mixed case with whitespace is normalized", language: "  Python\t", wantBinary: "scip-python", wantOK: true},
		{name: "unknown language", language: "cobol", wantBinary: "", wantOK: false},
		{name: "empty string", language: "", wantBinary: "", wantOK: false},
		{name: "whitespace only", language: "   ", wantBinary: "", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotBinary, gotOK := BinaryForLanguage(tt.language)
			if gotBinary != tt.wantBinary || gotOK != tt.wantOK {
				t.Fatalf("BinaryForLanguage(%q) = (%q, %t), want (%q, %t)",
					tt.language, gotBinary, gotOK, tt.wantBinary, tt.wantOK)
			}
		})
	}
}
