// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	impactdigest "github.com/eshu-hq/eshu/go/internal/ifa/impact/digest"
)

// TestImpactDigestShellCassetteListMatchesTheGoList pins the Compose cell's cassette list to
// impactdigest.Cassettes, the list the in-process family drives: if the two drifted, the cell and the
// family would digest different inputs and the shared pin would be meaningless.
func TestImpactDigestShellCassetteListMatchesTheGoList(t *testing.T) {
	t.Parallel()

	script, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "lib", "ifa_fault_injection_impact_cells.sh"))
	if err != nil {
		t.Fatalf("read the impact cell script: %v", err)
	}
	block := regexp.MustCompile(`(?s)impact_family_cassettes=\((.*?)\)`).FindSubmatch(script)
	if block == nil {
		t.Fatal("the impact cell script declares no impact_family_cassettes array")
	}
	var got []string
	for _, m := range regexp.MustCompile(`"testdata/cassettes/([^"]+)"`).FindAllSubmatch(block[1], -1) {
		got = append(got, string(m[1]))
	}
	if len(got) != len(impactdigest.Cassettes) {
		t.Fatalf("shell cassettes = %v, want %v", got, impactdigest.Cassettes)
	}
	for i := range got {
		if got[i] != impactdigest.Cassettes[i] {
			t.Fatalf("shell cassette %d = %q, want %q", i, got[i], impactdigest.Cassettes[i])
		}
	}
}

// TestImpactDigestFlagsRejectTwoPins pins the flag contract: one pin source at a time, and a pin file that
// pins nothing yields no expectation (the cell, not the verb, requires the pin).
func TestImpactDigestFlagsRejectTwoPins(t *testing.T) {
	t.Parallel()

	if _, err := parseImpactDigestFlags([]string{"-expect", "abc", "-expect-file", "x"}, io.Discard); err == nil {
		t.Fatal("parseImpactDigestFlags() error = nil, want -expect and -expect-file to be exclusive")
	}
	pin := filepath.Join(t.TempDir(), "pin.txt")
	if err := os.WriteFile(pin, []byte("# nothing pinned\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := expectedImpactDigest(impactDigestOptions{expectFile: pin})
	if err != nil || got != "" {
		t.Fatalf("expectedImpactDigest(unpinned file) = %q, %v, want an empty expectation", got, err)
	}
	if err := os.WriteFile(pin, []byte("# note\nabc123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := expectedImpactDigest(impactDigestOptions{expectFile: pin}); err != nil || got != "abc123" {
		t.Fatalf("expectedImpactDigest(pinned file) = %q, %v, want abc123", got, err)
	}
}
