// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shard

import (
	"strings"
	"testing"
)

func TestParseScopeID(t *testing.T) {
	t.Parallel()

	full := strings.Repeat("01", MaxDepth/2)
	cases := []struct {
		id, base, bits string
		ok             bool
	}{
		{testBase + "#shard-0", testBase, "0", true},
		{testBase + "#shard-10110", testBase, "10110", true},
		{testBase + "#shard-" + full, testBase, full, true},
		{testBase + "#shard-" + full + "0", "", "", false},
		{testBase, "", "", false},
		{testBase + "#shard-", "", "", false},
		{testBase + "#shard-012", "", "", false},
		{"#shard-01", "", "", false},
		{testBase + "#shard-01#shard-1", "", "", false},
	}
	for _, c := range cases {
		base, bits, ok := ParseScopeID(c.id)
		if base != c.base || bits != c.bits || ok != c.ok {
			t.Errorf("ParseScopeID(%q) = (%q, %q, %v), want (%q, %q, %v)", c.id, base, bits, ok, c.base, c.bits, c.ok)
		}
	}
}

func TestScopeIDRoundTripsAndStaysBounded(t *testing.T) {
	t.Parallel()

	full := strings.Repeat("1", MaxDepth)
	id := ScopeID(testBase, full)
	if len(id)-len(testBase) != MaxSuffixLength {
		t.Fatalf("suffix of %q is %d bytes, want %d", id, len(id)-len(testBase), MaxSuffixLength)
	}
	if base, bits, ok := ParseScopeID(id); !ok || base != testBase || bits != full {
		t.Fatalf("round trip of %q = (%q, %q, %v)", id, base, bits, ok)
	}
}

func TestHasMarker(t *testing.T) {
	t.Parallel()

	for id, want := range map[string]bool{
		testBase:                     false,
		testBase + "#shard-0":        true,
		"x#shard-":                   true,
		"security-alert#shardless":   false,
		"security-alert:#SHARD-0":    false,
		"  " + testBase + "#shard-1": true,
	} {
		if got := HasMarker(id); got != want {
			t.Errorf("HasMarker(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestNormalizeRepository(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"Acme/Repo":        "acme/repo",
		" /acme/repo.git/": "acme/repo",
		"acme/repo.GIT":    "acme/repo.git",
		"acme":             "",
		"acme/":            "",
		"/repo":            "",
		"a/b/c":            "",
		"":                 "",
	} {
		if got := NormalizeRepository(in); got != want {
			t.Errorf("NormalizeRepository(%q) = %q, want %q", in, got, want)
		}
	}
}
