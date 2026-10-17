// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

func TestParseTrailerLine(t *testing.T) {
	cases := []struct {
		name, line, wantErr string
		wantN               int
		wantReason          string
	}{
		{"valid", "Dirgate-Growth: internal/foo +3 #6054 split lands next", "", 3, "split lands next"},
		{"valid cross repo issue", "Dirgate-Growth: a/b +1 eshu-hq/eshu#6054 why", "", 1, "why"},
		{"case insensitive key", "dirgate-growth: a/b +1 #1 why", "", 1, "why"},
		{"zero", "Dirgate-Growth: a/b +0 #1 why", "growth", 0, ""},
		{"no plus", "Dirgate-Growth: a/b 2 #1 why", "growth", 0, ""},
		{"no issue", "Dirgate-Growth: a/b +2 because", "issue", 2, ""},
		{"no reason", "Dirgate-Growth: a/b +2 #9", "reason", 2, ""},
		{"only dir", "Dirgate-Growth: a/b", "missing", 0, ""},
		{"empty", "Dirgate-Growth:", "missing", 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseTrailerLine("c0ffee", tc.line)
			if tc.wantErr == "" {
				if got.err != "" || got.n != tc.wantN || got.reason != tc.wantReason {
					t.Fatalf("got %+v", got)
				}
				return
			}
			if got.err == "" || !contains(got.err, tc.wantErr) {
				t.Fatalf("err = %q, want it to mention %q", got.err, tc.wantErr)
			}
		})
	}
}

func TestParseTrailersIgnoresOtherLines(t *testing.T) {
	msg := "subject\n\nbody mentions Dirgate-Growth: not at line start\nDirgate-Growth: a +1 #1 r\nSigned-off-by: x\n"
	got := parseTrailers("abc", msg)
	if len(got) != 1 || got[0].dir != "a" {
		t.Fatalf("got %+v", got)
	}
}

func TestMarkerReason(t *testing.T) {
	cases := []struct {
		line                   string
		reason                 string
		hasReason, wantPresent bool
	}{
		{"package x //nolint:dirgate // legacy wire name", "legacy wire name", true, true},
		{"package x //nolint:dirgate", "", false, true},
		{"package x //nolint:dirgate //", "", false, true},
		{"package x //nolint:dirgateway // other linter", "", false, false},
		{"package x // no marker", "", false, false},
		{"var s = \"//nolint:dirgate // x\"", "", false, false},
	}
	for _, tc := range cases {
		reason, has, present := markerReason(tc.line)
		if reason != tc.reason || has != tc.hasReason || present != tc.wantPresent {
			t.Errorf("%q: got (%q,%v,%v)", tc.line, reason, has, present)
		}
	}
}

func TestParseSnapshotCensus(t *testing.T) {
	s := parseSnapshot([]string{
		"go/a/x.go", "go/a/x_test.go", "go/a/sub/y.go", "go/a/testdata/z.go",
		"go/a/.hidden/h.go", "go/a/README.md", "go/root.go", "docs/other.go",
	})
	if s.count("a") != 1 || s.count("a/sub") != 1 || s.count(".") != 1 {
		t.Fatalf("counts: %+v", s.qualifying)
	}
	if s.count("a/testdata") != 0 || s.count("a/.hidden") != 0 {
		t.Fatalf("skipped directories counted: %+v", s.qualifying)
	}
	if got := s.subpackages["a"]; len(got) != 1 || got[0] != "sub" {
		t.Fatalf("subpackages = %v", got)
	}
	if _, ok := s.allDirs["a"]; !ok {
		t.Fatal("ancestor directory missing from allDirs")
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
