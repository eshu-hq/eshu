// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNolintJustification(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantOK  bool
		wantMsg string
	}{
		{
			name:    "justified same-line marker",
			content: "package foo //nolint:dirgate // 92 files; see #1234 for the split plan\n",
			wantOK:  true,
			wantMsg: "92 files; see #1234 for the split plan",
		},
		{
			name:    "bare marker with no justification is not accepted",
			content: "package foo //nolint:dirgate\n",
			wantOK:  false,
		},
		{
			name:    "marker with an empty trailing comment is not accepted",
			content: "package foo //nolint:dirgate //\n",
			wantOK:  false,
		},
		{
			name:    "marker with a whitespace-only trailing comment is not accepted",
			content: "package foo //nolint:dirgate //    \n",
			wantOK:  false,
		},
		{
			name:    "no marker at all",
			content: "package foo\n",
			wantOK:  false,
		},
		{
			name:    "a different gate's marker does not suppress dirgate",
			content: "package foo //nolint:filelength // 900 lines, tracked in #1\n",
			wantOK:  false,
		},
		{
			name:    "leading and trailing whitespace around the justification is trimmed",
			content: "package foo //nolint:dirgate //   spaced reason   \n",
			wantOK:  true,
			wantMsg: "spaced reason",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "fixture.go")
			if err := os.WriteFile(path, []byte(c.content), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}

			gotMsg, gotOK := nolintJustification(path, "dirgate")
			if gotOK != c.wantOK {
				t.Fatalf("nolintJustification ok = %v, want %v", gotOK, c.wantOK)
			}
			if gotOK && gotMsg != c.wantMsg {
				t.Fatalf("nolintJustification justification = %q, want %q", gotMsg, c.wantMsg)
			}
		})
	}
}

func TestNolintJustificationMissingFile(t *testing.T) {
	_, ok := nolintJustification(filepath.Join(t.TempDir(), "does-not-exist.go"), "dirgate")
	if ok {
		t.Fatal("nolintJustification on a missing file reported a justification")
	}
}
