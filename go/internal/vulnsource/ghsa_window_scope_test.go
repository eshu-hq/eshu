// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnsource

import (
	"testing"
	"time"
)

func TestGHSAWindowScopeRoundTrip(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	scopeID := GHSAWindowScopeID("reviewed", start, end)
	want := "vuln-intel://ghsa/reviewed/_window/1793491200000000-1793577600000000"
	if scopeID != want {
		t.Fatalf("GHSAWindowScopeID() = %q, want %q", scopeID, want)
	}
	advisoryType, gotStart, gotEnd, ok := ParseGHSAWindowScope(scopeID)
	if !ok || advisoryType != "reviewed" || !gotStart.Equal(start) || !gotEnd.Equal(end) {
		t.Fatalf("ParseGHSAWindowScope(%q) = (%q, %v, %v, %t)", scopeID, advisoryType, gotStart, gotEnd, ok)
	}
	if !IsGHSAWindowScope(scopeID) {
		t.Fatalf("IsGHSAWindowScope(%q) = false", scopeID)
	}
	stream, ok := GHSAStreamScopeID(scopeID)
	if !ok || stream != "vuln-intel://ghsa/reviewed" {
		t.Fatalf("GHSAStreamScopeID(%q) = (%q, %t)", scopeID, stream, ok)
	}
	if src, ok := SourceForScopeID(scopeID); !ok || src != SourceGHSA {
		t.Fatalf("SourceForScopeID(window) = (%q, %t), want ghsa", src, ok)
	}
}

func TestGHSAWindowScopeRefusesEverythingElse(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	// Degenerate mints refuse.
	for name, got := range map[string]string{
		"bad type":    GHSAWindowScopeID("unreviewed", start, start.Add(time.Hour)),
		"empty type":  GHSAWindowScopeID("", start, start.Add(time.Hour)),
		"zero window": GHSAWindowScopeID("reviewed", start, start),
		"reversed":    GHSAWindowScopeID("malware", start.Add(time.Hour), start),
	} {
		if got != "" {
			t.Errorf("%s: GHSAWindowScopeID() = %q, want empty", name, got)
		}
	}
	// Every non-grammar scope refuses to parse and invents no source token.
	for _, scopeID := range []string{
		"vuln-intel://ghsa/reviewed/_window/abc-def",
		"vuln-intel://ghsa/reviewed/_window/1793577600000000-1793491200000000",
		"vuln-intel://ghsa/reviewed/_window/1793491200000000",
		"vuln-intel://ghsa/reviewed/_window/1793491200000000-1793577600000000/extra",
		"vuln-intel://ghsa/reviewed/_window/1793491200000000-1793577600000000?x=1",
		"vuln-intel://ghsa/unreviewed/_window/1793491200000000-1793577600000000",
		"vuln-intel://ghsa/reviewedx/_window/1793491200000000-1793577600000000",
		"vuln-intel://ghsa/reviewed/_window/-1793577600000000",
		"vuln-intel://ghsa/reviewed/_window/0-1793577600000000",
		"vuln-intel://ghsa/reviewed/_window/1793491200000000-1793491200000000",
		"vuln-intel://nvd/_window/1793491200000000-1793577600000000",
		"vuln-intel://ghsa/reviewed",
	} {
		if _, _, _, ok := ParseGHSAWindowScope(scopeID); ok {
			t.Errorf("ParseGHSAWindowScope(%q) ok = true, want false", scopeID)
		}
		if IsGHSAWindowScope(scopeID) {
			t.Errorf("IsGHSAWindowScope(%q) = true, want false", scopeID)
		}
	}
	// The anchor still maps to ghsa; window look-alikes do not.
	if _, ok := SourceForScopeID("vuln-intel://ghsa/reviewed"); !ok {
		t.Error("SourceForScopeID(anchor) ok = false, want true")
	}
	if _, ok := SourceForScopeID("vuln-intel://ghsa/reviewed/_window/abc-def"); ok {
		t.Error("SourceForScopeID(malformed window) ok = true, want false")
	}
	if _, ok := GHSAStreamScopeID("vuln-intel://ghsa/reviewed"); ok {
		t.Error("GHSAStreamScopeID(anchor) ok = true, want false for a non-window scope")
	}
}
