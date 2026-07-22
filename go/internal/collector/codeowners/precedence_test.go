// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeowners

import "testing"

func TestResolveWinner(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		candidates     map[string]string
		wantSourcePath string
		wantBody       string
		wantOK         bool
	}{
		{
			name:       "no candidates present",
			candidates: map[string]string{},
			wantOK:     false,
		},
		{
			name:       "nil candidate map",
			candidates: nil,
			wantOK:     false,
		},
		{
			name: "only docs CODEOWNERS present",
			candidates: map[string]string{
				"docs/CODEOWNERS": "docs body",
			},
			wantSourcePath: "docs/CODEOWNERS",
			wantBody:       "docs body",
			wantOK:         true,
		},
		{
			name: "only root CODEOWNERS present",
			candidates: map[string]string{
				"CODEOWNERS": "root body",
			},
			wantSourcePath: "CODEOWNERS",
			wantBody:       "root body",
			wantOK:         true,
		},
		{
			name: "only github CODEOWNERS present",
			candidates: map[string]string{
				".github/CODEOWNERS": "github body",
			},
			wantSourcePath: ".github/CODEOWNERS",
			wantBody:       "github body",
			wantOK:         true,
		},
		{
			name: "root beats docs",
			candidates: map[string]string{
				"CODEOWNERS":      "root body",
				"docs/CODEOWNERS": "docs body",
			},
			wantSourcePath: "CODEOWNERS",
			wantBody:       "root body",
			wantOK:         true,
		},
		{
			name: "github beats root",
			candidates: map[string]string{
				".github/CODEOWNERS": "github body",
				"CODEOWNERS":         "root body",
			},
			wantSourcePath: ".github/CODEOWNERS",
			wantBody:       "github body",
			wantOK:         true,
		},
		{
			name: "all three present github wins",
			candidates: map[string]string{
				".github/CODEOWNERS": "github body",
				"CODEOWNERS":         "root body",
				"docs/CODEOWNERS":    "docs body",
			},
			wantSourcePath: ".github/CODEOWNERS",
			wantBody:       "github body",
			wantOK:         true,
		},
		{
			name: "unrecognized path is ignored",
			candidates: map[string]string{
				"some/other/CODEOWNERS": "ignored body",
			},
			wantOK: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			gotSourcePath, gotBody, gotOK := ResolveWinner(testCase.candidates)
			if gotOK != testCase.wantOK {
				t.Fatalf("ResolveWinner() ok = %t, want %t", gotOK, testCase.wantOK)
			}
			if gotSourcePath != testCase.wantSourcePath {
				t.Fatalf("ResolveWinner() sourcePath = %q, want %q", gotSourcePath, testCase.wantSourcePath)
			}
			if gotBody != testCase.wantBody {
				t.Fatalf("ResolveWinner() body = %q, want %q", gotBody, testCase.wantBody)
			}
		})
	}
}

func TestIsCandidatePath(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		relPath string
		want    string
		wantOK  bool
	}{
		{name: "github path", relPath: ".github/CODEOWNERS", want: ".github/CODEOWNERS", wantOK: true},
		{name: "root path", relPath: "CODEOWNERS", want: "CODEOWNERS", wantOK: true},
		{name: "docs path", relPath: "docs/CODEOWNERS", want: "docs/CODEOWNERS", wantOK: true},
		{name: "case sensitive mismatch is rejected", relPath: "codeowners", wantOK: false},
		{name: "nested docs path is rejected", relPath: "src/docs/CODEOWNERS", wantOK: false},
		{name: "wrong directory is rejected", relPath: "github/CODEOWNERS", wantOK: false},
		{name: "unrelated file is rejected", relPath: "README.md", wantOK: false},
		{name: "empty path is rejected", relPath: "", wantOK: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, gotOK := IsCandidatePath(testCase.relPath)
			if gotOK != testCase.wantOK {
				t.Fatalf("IsCandidatePath(%q) ok = %t, want %t", testCase.relPath, gotOK, testCase.wantOK)
			}
			if got != testCase.want {
				t.Fatalf("IsCandidatePath(%q) = %q, want %q", testCase.relPath, got, testCase.want)
			}
		})
	}
}
