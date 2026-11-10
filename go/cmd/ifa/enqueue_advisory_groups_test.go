// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"io"
	"strings"
	"testing"
)

// TestParseEnqueueAdvisoryGroupsFlags pins the verb's fail-closed flag
// contract: a run that names no group, or no origin scope, would admit
// nothing (or admit it under an invented origin), so both must red
// before any database is touched.
func TestParseEnqueueAdvisoryGroupsFlags(t *testing.T) {
	t.Parallel()

	ok, err := parseEnqueueAdvisoryGroupsFlags([]string{
		"-cassette", "testdata/cassettes/advisorygroup/ifa-advisory-group-family.json",
		"-group", "CVE-2026-10466", "-group", "GHSA-10466-aaaa-bbbb",
		"-scope", "osv:eshu-fixture-advisory-findings",
		"-generation", "gen-ifa-advisory-group-family-findings-1",
		"-source-system", "osv",
	}, io.Discard)
	if err != nil {
		t.Fatalf("valid flags: %v", err)
	}
	if len(ok.groups) != 2 || ok.groups[0] != "CVE-2026-10466" || ok.groups[1] != "GHSA-10466-aaaa-bbbb" {
		t.Fatalf("groups = %v, want both -group values in order", ok.groups)
	}

	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "no cassette", args: []string{"-group", "CVE-1", "-scope", "s", "-generation", "g", "-source-system", "osv"}, want: "-cassette is required"},
		{name: "no group", args: []string{"-cassette", "c.json", "-scope", "s", "-generation", "g", "-source-system", "osv"}, want: "at least one -group is required"},
		{name: "blank group", args: []string{"-cassette", "c.json", "-group", "  ", "-scope", "s", "-generation", "g", "-source-system", "osv"}, want: "must not be blank"},
		{name: "no scope", args: []string{"-cassette", "c.json", "-group", "CVE-1", "-generation", "g", "-source-system", "osv"}, want: "-scope is required"},
		{name: "no generation", args: []string{"-cassette", "c.json", "-group", "CVE-1", "-scope", "s", "-source-system", "osv"}, want: "-generation is required"},
		{name: "no source system", args: []string{"-cassette", "c.json", "-group", "CVE-1", "-scope", "s", "-generation", "g"}, want: "-source-system is required"},
		{name: "positional", args: []string{"-cassette", "c.json", "-group", "CVE-1", "-scope", "s", "-generation", "g", "-source-system", "osv", "extra"}, want: "positional arguments are not accepted"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := parseEnqueueAdvisoryGroupsFlags(tc.args, io.Discard); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want substring %q", err, tc.want)
			}
		})
	}
}
