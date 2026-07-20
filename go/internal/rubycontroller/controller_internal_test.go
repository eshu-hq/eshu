// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rubycontroller

import (
	"sort"
	"strings"
	"testing"
)

// internalFakeRegistry mirrors the external test fake but lives in-package so it
// can drive the unexported confirm-only probe directly.
type internalFakeRegistry struct {
	classes map[string][]string
}

func iSegs(s string) []string { return strings.Split(s, "::") }

func iTailEq(a, b []string) bool {
	off := len(a) - len(b)
	if off < 0 {
		return false
	}
	for i := range b {
		if a[off+i] != b[i] {
			return false
		}
	}
	return true
}

func (r internalFakeRegistry) ExactMatches(ref string) []string {
	rs := iSegs(ref)
	var out []string
	for k := range r.classes {
		if ks := iSegs(k); len(ks) == len(rs) && iTailEq(ks, rs) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func (r internalFakeRegistry) SuffixMatches(ref string) []string {
	rs := iSegs(ref)
	var out []string
	for k := range r.classes {
		if ks := iSegs(k); len(ks) > len(rs) && iTailEq(ks, rs) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func (r internalFakeRegistry) EntryMatches(ctx string) []string {
	cs := iSegs(ctx)
	var out []string
	for k := range r.classes {
		if ks := iSegs(k); len(ks) >= len(cs) && iTailEq(ks, cs) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func (r internalFakeRegistry) DeclaredBasesOf(classKey string) ([]string, bool) {
	b, ok := r.classes[classKey]
	if !ok || len(b) == 0 {
		return nil, false
	}
	return b, true
}

// TestProbeClassConfirmIsolation proves the confirm-only probe returns TRUE only
// when a path reaches the accepted controller-base set, and structurally cannot
// contribute a downgrade: a suffix candidate whose chain hits the reject-set (or
// any non-controller terminal) returns FALSE, never a downgrade signal.
func TestProbeClassConfirmIsolation(t *testing.T) {
	t.Run("downgrading candidate contributes no confirm", func(t *testing.T) {
		reg := internalFakeRegistry{classes: map[string][]string{
			"Internal::Api::Base": {"ActiveRecord::Base"},
		}}
		if probeClassConfirm("Internal::Api::Base", reg, map[string]struct{}{}, 0) {
			t.Fatal("probe must not confirm a class whose chain reaches the reject-set")
		}
	})

	t.Run("candidate reaching accepted confirms", func(t *testing.T) {
		reg := internalFakeRegistry{classes: map[string][]string{
			"Admin::Base": {"ActionController::Base"},
		}}
		if !probeClassConfirm("Admin::Base", reg, map[string]struct{}{}, 0) {
			t.Fatal("probe must confirm a class whose chain reaches the accepted set")
		}
	})

	t.Run("cycle terminates without confirm", func(t *testing.T) {
		reg := internalFakeRegistry{classes: map[string][]string{
			"X": {"Y"},
			"Y": {"X"},
		}}
		if probeClassConfirm("X", reg, map[string]struct{}{}, 0) {
			t.Fatal("a cycle that never reaches the accepted set must return false")
		}
	})

	t.Run("depth cap blocks a too-deep confirm", func(t *testing.T) {
		classes := map[string][]string{"C0": {"C1"}}
		for i := 1; i <= 11; i++ {
			classes[itoaN("C", i)] = []string{itoaN("C", i+1)}
		}
		classes["C12"] = []string{"ActionController::Base"}
		if probeClassConfirm("C0", internalFakeRegistry{classes: classes}, map[string]struct{}{}, 0) {
			t.Fatal("an accepted base beyond MaxWalkDepth must not confirm through the probe")
		}
	})
}

func itoaN(prefix string, n int) string {
	digits := ""
	if n == 0 {
		digits = "0"
	}
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return prefix + digits
}
