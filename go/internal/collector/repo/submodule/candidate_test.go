// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package submodule

import "testing"

func TestIsGitmodulesPath(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		relPath string
		want    bool
	}{
		{name: "root path matches", relPath: ".gitmodules", want: true},
		{name: "case sensitive mismatch is rejected", relPath: ".GitModules", want: false},
		{name: "nested path is rejected", relPath: "vendor/.gitmodules", want: false},
		{name: "unrelated file is rejected", relPath: "README.md", want: false},
		{name: "empty path is rejected", relPath: "", want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := IsGitmodulesPath(testCase.relPath); got != testCase.want {
				t.Fatalf("IsGitmodulesPath(%q) = %t, want %t", testCase.relPath, got, testCase.want)
			}
		})
	}
}
