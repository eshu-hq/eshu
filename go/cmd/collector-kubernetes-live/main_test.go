// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

func TestParseArgs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		args     []string
		wantMode launchMode
		wantFile string
		wantErr  bool
	}{
		{name: "default is live", args: nil, wantMode: launchModeLive},
		{name: "explicit live", args: []string{"-mode", "live"}, wantMode: launchModeLive},
		{
			name:     "cassette requires file",
			args:     []string{"-mode", "cassette", "-cassette-file", "/tmp/c.json"},
			wantMode: launchModeCassette,
			wantFile: "/tmp/c.json",
		},
		{
			name:    "cassette without file errors",
			args:    []string{"-mode", "cassette"},
			wantErr: true,
		},
		{
			name:     "record requires file",
			args:     []string{"-mode", "record", "-cassette-file", "/tmp/out.json"},
			wantMode: launchModeRecord,
			wantFile: "/tmp/out.json",
		},
		{
			name:    "record without file errors",
			args:    []string{"-mode", "record"},
			wantErr: true,
		},
		{
			name:    "unknown mode errors",
			args:    []string{"-mode", "bogus"},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts, err := parseArgs(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseArgs(%v) = nil error, want error", tc.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseArgs(%v) error = %v", tc.args, err)
			}
			if opts.mode != tc.wantMode {
				t.Errorf("mode = %q, want %q", opts.mode, tc.wantMode)
			}
			if opts.cassetteFile != tc.wantFile {
				t.Errorf("cassetteFile = %q, want %q", opts.cassetteFile, tc.wantFile)
			}
		})
	}
}
