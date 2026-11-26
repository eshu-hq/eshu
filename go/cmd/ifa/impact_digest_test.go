// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

// TestImpactDigestErrorPathsAreHermetic pins the flag-before-backend ordering
// the AGENTS.md invariants require of every verb: a bad flag, a missing pin
// file, and a missing Postgres DSN each fail before any database is opened,
// so these tests run with no Docker, no Postgres, and no network.
func TestImpactDigestErrorPathsAreHermetic(t *testing.T) {
	t.Parallel()

	t.Run("bad flag", func(t *testing.T) {
		t.Parallel()

		if _, err := parseImpactDigestFlags([]string{"-bogus-flag"}, io.Discard); err == nil {
			t.Fatal("parseImpactDigestFlags(-bogus-flag) = nil error, want a flag error")
		}
		var stdout, stderr bytes.Buffer
		err := runImpactDigestCommand(context.Background(), []string{"-bogus-flag"}, &stdout, &stderr)
		if err == nil {
			t.Fatal("runImpactDigestCommand(-bogus-flag) = nil error, want a flag error")
		}
	})

	t.Run("valid flags parse", func(t *testing.T) {
		t.Parallel()

		o, err := parseImpactDigestFlags([]string{"-expect", "abc123"}, io.Discard)
		if err != nil {
			t.Fatalf("parseImpactDigestFlags(-expect) error = %v", err)
		}
		if o.expect != "abc123" {
			t.Fatalf("parseImpactDigestFlags(-expect) expect = %q, want abc123", o.expect)
		}
	})

	t.Run("expect is trimmed", func(t *testing.T) {
		t.Parallel()

		got, err := expectedImpactDigest(impactDigestOptions{expect: "  abc123\n"})
		if err != nil || got != "abc123" {
			t.Fatalf("expectedImpactDigest(padded expect) = %q, %v, want abc123", got, err)
		}
	})

	t.Run("missing pin file", func(t *testing.T) {
		t.Parallel()

		missing := filepath.Join(t.TempDir(), "does-not-exist.txt")
		if _, err := expectedImpactDigest(impactDigestOptions{expectFile: missing}); err == nil {
			t.Fatal("expectedImpactDigest(missing file) = nil error, want a read error")
		} else if !strings.Contains(err.Error(), missing) {
			t.Fatalf("error = %v, want it to name the missing pin file %q", err, missing)
		}
		var stdout, stderr bytes.Buffer
		err := runImpactDigestCommand(context.Background(), []string{"-expect-file", missing}, &stdout, &stderr)
		if err == nil {
			t.Fatal("runImpactDigestCommand(missing pin file) = nil error, want a read error")
		} else if !strings.Contains(err.Error(), "read -expect-file") {
			t.Fatalf("error = %v, want it to wrap the pin-file read", err)
		}
	})

	t.Run("exclusive pins", func(t *testing.T) {
		t.Parallel()

		var stdout, stderr bytes.Buffer
		err := runImpactDigestCommand(context.Background(),
			[]string{"-expect", "abc", "-expect-file", "x"}, &stdout, &stderr)
		if err == nil {
			t.Fatal("runImpactDigestCommand(two pins) = nil error, want the exclusivity error")
		} else if !strings.Contains(err.Error(), "exclusive") {
			t.Fatalf("error = %v, want the -expect/-expect-file exclusivity error", err)
		}
	})
}

// TestRunImpactDigestCommandRequiresPostgresDSN proves the verb opens Postgres
// only after its inputs check out, and names the missing DSN when no database
// is configured. It unsets the DSN keys so it stays hermetic even on a machine
// with a live database configured.
func TestRunImpactDigestCommandRequiresPostgresDSN(t *testing.T) {
	for _, key := range []string{"ESHU_FACT_STORE_DSN", "ESHU_CONTENT_STORE_DSN", "ESHU_POSTGRES_DSN"} {
		t.Setenv(key, "")
	}

	var stdout, stderr bytes.Buffer
	err := runImpactDigestCommand(context.Background(), []string{"-expect", "abc123"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("runImpactDigestCommand without a DSN = nil error, want the missing-DSN error")
	} else if !strings.Contains(err.Error(), "open postgres") {
		t.Fatalf("error = %v, want it to name the postgres open", err)
	}
}

// TestRunDispatchesImpactDigest pins the main dispatch entry for the verb: a
// bad flag through the top-level run still reaches runImpactDigestCommand and
// fails on the flag, proving the case arm is wired without opening a backend.
func TestRunDispatchesImpactDigest(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"impact-digest", "-bogus-flag"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(impact-digest -bogus-flag) = nil error, want a flag error")
	}
}
