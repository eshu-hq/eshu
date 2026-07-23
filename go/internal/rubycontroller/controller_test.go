// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rubycontroller_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/rubycontroller"
)

// fakeRegistry is a repo-wide-style registry keyed by qualified class name
// (classKey). A nil base slice marks a base-less known class. It implements the
// identity-carrying Registry with real offset-0 (exact) vs offset>0 (strict
// suffix) matching so the #5376 P0 rev-2 semantics can be exercised directly.
type fakeRegistry struct {
	classes map[string][]string
}

func fakeSegs(s string) []string { return strings.Split(s, "::") }

func fakeTailEq(a, b []string) bool {
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

func (r fakeRegistry) ExactMatches(ref string) []string {
	rs := fakeSegs(ref)
	var out []string
	for k := range r.classes {
		if ks := fakeSegs(k); len(ks) == len(rs) && fakeTailEq(ks, rs) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func (r fakeRegistry) SuffixMatches(ref string) []string {
	rs := fakeSegs(ref)
	var out []string
	for k := range r.classes {
		if ks := fakeSegs(k); len(ks) > len(rs) && fakeTailEq(ks, rs) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func (r fakeRegistry) EntryMatches(ctx string) []string {
	cs := fakeSegs(ctx)
	var out []string
	for k := range r.classes {
		if ks := fakeSegs(k); len(ks) >= len(cs) && fakeTailEq(ks, cs) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func (r fakeRegistry) DeclaredBasesOf(classKey string) ([]string, bool) {
	b, ok := r.classes[classKey]
	if !ok || len(b) == 0 {
		return nil, false
	}
	return b, true
}

func TestDecide(t *testing.T) {
	tests := []struct {
		name       string
		class      string
		classes    map[string][]string
		wantKeep   bool
		wantReason string
	}{
		{
			name:       "direct accepted base",
			class:      "WidgetsController",
			classes:    map[string][]string{"WidgetsController": {"ApplicationController"}},
			wantKeep:   true,
			wantReason: rubycontroller.ReasonAccepted,
		},
		{
			name:       "exact chain resolves to accepted base",
			class:      "OrdersController",
			classes:    map[string][]string{"OrdersController": {"Admin::Base"}, "Admin::Base": {"ActionController::Base"}},
			wantKeep:   true,
			wantReason: rubycontroller.ReasonAccepted,
		},
		{
			name:       "exact chain resolves onward to rejected framework base",
			class:      "FooController",
			classes:    map[string][]string{"FooController": {"ApplicationRecord"}, "ApplicationRecord": {"ActiveRecord::Base"}},
			wantKeep:   false,
			wantReason: rubycontroller.ReasonRejectedFrameworkBase,
		},
		{
			// #5376 P0: Api::Base (real gem base) has NO exact corpus match, only a
			// proper-suffix impostor Internal::Api::Base < ActiveRecord::Base. The
			// suffix match may not feed a downgrade -> KEEP.
			name:       "suffix-only impostor keeps (no probe confirm)",
			class:      "OrdersController",
			classes:    map[string][]string{"OrdersController": {"Api::Base"}, "Internal::Api::Base": {"ActiveRecord::Base"}},
			wantKeep:   true,
			wantReason: rubycontroller.ReasonSuffixOnlyAmbiguous,
		},
		{
			name:       "suffix-only probe confirms to accepted",
			class:      "OrdersController",
			classes:    map[string][]string{"OrdersController": {"Base"}, "Admin::Base": {"ActionController::Base"}},
			wantKeep:   true,
			wantReason: rubycontroller.ReasonAccepted,
		},
		{
			name:       "unresolved qualified base keeps (F1 floor)",
			class:      "OrdersController",
			classes:    map[string][]string{"OrdersController": {"Sinatra::Base"}},
			wantKeep:   true,
			wantReason: rubycontroller.ReasonUnresolvedQualified,
		},
		{
			name:       "conventional simple base with zero candidates keeps",
			class:      "FooController",
			classes:    map[string][]string{"FooController": {"Base"}},
			wantKeep:   true,
			wantReason: rubycontroller.ReasonSuffixOnlyAmbiguous,
		},
		{
			name:       "unresolved simple non-controller base downgrades",
			class:      "ReportController",
			classes:    map[string][]string{"ReportController": {"Thor"}},
			wantKeep:   false,
			wantReason: rubycontroller.ReasonUnresolvedNonController,
		},
		{
			name:       "unresolved simple Controller-suffixed base keeps",
			class:      "OrdersController",
			classes:    map[string][]string{"OrdersController": {"BaseController"}},
			wantKeep:   true,
			wantReason: rubycontroller.ReasonUnresolvedController,
		},
		{
			// step 3 (suffix probe) is checked BEFORE step 4 (reject-set): a corpus
			// Legacy::ActiveRecord::Base shadows the literal ActiveRecord::Base ref.
			name:       "reject-set shadowed by suffix candidate keeps",
			class:      "FooController",
			classes:    map[string][]string{"FooController": {"ActiveRecord::Base"}, "Legacy::ActiveRecord::Base": {"SomeGem::Thing"}},
			wantKeep:   true,
			wantReason: rubycontroller.ReasonSuffixOnlyAmbiguous,
		},
		{
			// exact-match contamination: the offset-0 impostor Base<ActiveRecord::Base
			// is beaten by the suffix candidate Api::V1::Base<ActionController::API
			// via any-path-keeps.
			name:       "exact impostor beaten by confirming suffix candidate",
			class:      "OrdersController",
			classes:    map[string][]string{"OrdersController": {"Base"}, "Base": {"ActiveRecord::Base"}, "Api::V1::Base": {"ActionController::API"}},
			wantKeep:   true,
			wantReason: rubycontroller.ReasonAccepted,
		},
		{
			name:       "collision all candidate paths downgrade",
			class:      "BaseController",
			classes:    map[string][]string{"BaseController": {"ApplicationRecord", "Thor"}},
			wantKeep:   false,
			wantReason: rubycontroller.ReasonCollision,
		},
		{
			name:       "cycle is keep-biased for controller name",
			class:      "FooController",
			classes:    map[string][]string{"FooController": {"BarController"}, "BarController": {"FooController"}},
			wantKeep:   true,
			wantReason: rubycontroller.ReasonCycle,
		},
		{
			// #5500: lexical-scope-aware candidate restriction. "Base" is declared
			// as OrdersController's superclass ref inside namespace "Admin"
			// (OrdersController's own qualified name is "Admin::OrdersController").
			// A same-lexical-scope corpus class "Admin::Base" resolves onward to
			// the reject-set. Pre-#5500, "Base" had zero ExactMatches and one
			// broad, unscoped SuffixMatches candidate ("Admin::Base"); since the
			// probe-confirm on that candidate does not reach an accepted base, the
			// old code kept it ambiguous forever (ReasonSuffixOnlyAmbiguous) even
			// though the TRUE referent (known via lexical scope) definitively
			// proves this is not a controller. The lexical restriction resolves
			// "Base" to the exact identity "Admin::Base" and lets it downgrade.
			name:       "lexical-scope match resolves exactly and downgrades",
			class:      "Admin::OrdersController",
			classes:    map[string][]string{"Admin::OrdersController": {"Base"}, "Admin::Base": {"ActiveRecord::Base"}},
			wantKeep:   false,
			wantReason: rubycontroller.ReasonRejectedFrameworkBase,
		},
		{
			// #5500: outer-enclosing-scope preservation. "Base" is referenced
			// inside Admin::V1::OrdersController; the true referent Admin::Base is
			// declared ONE lexical level out (namespace "Admin", not "Admin::V1").
			// Real Ruby constant lookup walks Module.nesting from innermost to
			// outermost before falling to top-level, so this must still resolve
			// through the outer scope, not just the immediate enclosing one.
			name:       "lexical-scope match resolves through an outer enclosing scope",
			class:      "Admin::V1::OrdersController",
			classes:    map[string][]string{"Admin::V1::OrdersController": {"Base"}, "Admin::Base": {"ApplicationController"}},
			wantKeep:   true,
			wantReason: rubycontroller.ReasonAccepted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rubycontroller.Decide(tt.class, fakeRegistry{classes: tt.classes})
			if got.Keep != tt.wantKeep {
				t.Fatalf("Decide(%q).Keep = %v, want %v (decision=%+v)", tt.class, got.Keep, tt.wantKeep, got)
			}
			if got.Reason != tt.wantReason {
				t.Fatalf("Decide(%q).Reason = %q, want %q (decision=%+v)", tt.class, got.Reason, tt.wantReason, got)
			}
			if rubycontroller.IsRailsController(tt.class, fakeRegistry{classes: tt.classes}) != tt.wantKeep {
				t.Fatalf("IsRailsController(%q) disagrees with Decide().Keep", tt.class)
			}
		})
	}
}

// TestDecideLexicalScopeOuterMatchUsesExactPath proves the outer-enclosing-scope
// match from TestDecide's "resolves through an outer enclosing scope" case is
// reached via the NEW lexical-exact resolution path (Terminal
// "accepted:ApplicationController"), not merely via the pre-#5500 broad
// suffix-probe fallback (which would have produced the different terminal
// "accepted_via_suffix:Base"). Both paths happen to agree on Keep/Reason here,
// so this pins the actual mechanism engaged rather than an incidental match.
func TestDecideLexicalScopeOuterMatchUsesExactPath(t *testing.T) {
	classes := map[string][]string{
		"Admin::V1::OrdersController": {"Base"},
		"Admin::Base":                 {"ApplicationController"},
	}
	got := rubycontroller.Decide("Admin::V1::OrdersController", fakeRegistry{classes: classes})
	if !got.Keep || got.Reason != rubycontroller.ReasonAccepted {
		t.Fatalf("Decide = %+v, want Keep=true Reason=accepted", got)
	}
	wantTerminal := "accepted:ApplicationController"
	if got.Terminal != wantTerminal {
		t.Fatalf("Terminal = %q, want %q (lexical restriction must resolve to the exact Admin::Base identity, not the broad suffix-probe fallback)", got.Terminal, wantTerminal)
	}
}

// TestDecideLexicalScopeIsNoOpForTopLevelClass proves the lexical restriction
// never changes behavior for a top-level (non-namespaced) walked class: with no
// namespace to derive, candidate resolution degrades to exactly the pre-#5500
// ExactMatches(ref)/SuffixMatches(ref) lookup, keeping every existing top-level
// test case in this file green.
func TestDecideLexicalScopeIsNoOpForTopLevelClass(t *testing.T) {
	classes := map[string][]string{
		"OrdersController": {"Base"},
		"Admin::Base":      {"ActiveRecord::Base"},
		"Reporting::Base":  {"ActiveRecord::Base"},
	}
	got := rubycontroller.Decide("OrdersController", fakeRegistry{classes: classes})
	if !got.Keep || got.Reason != rubycontroller.ReasonSuffixOnlyAmbiguous {
		t.Fatalf("top-level walked class must keep the pre-#5500 broad-ambiguous outcome, got %+v", got)
	}
}

// TestDecideDepthCapIsKeepBiased builds an exact chain longer than MaxWalkDepth
// that would resolve to a reject-set base if fully walked, and proves the depth
// cap falls back to the keep-biased residual instead of downgrading.
func TestDecideDepthCapIsKeepBiased(t *testing.T) {
	classes := map[string][]string{"C0Controller": {"C1"}}
	for i := 1; i <= 11; i++ {
		classes[itoa("C", i)] = []string{itoa("C", i+1)}
	}
	classes["C12"] = []string{"ActiveRecord::Base"}
	got := rubycontroller.Decide("C0Controller", fakeRegistry{classes: classes})
	if !got.Keep {
		t.Fatalf("deep chain from a *Controller must keep at the depth cap, got %+v", got)
	}
	if got.Reason != rubycontroller.ReasonDepthCap {
		t.Fatalf("Reason = %q, want %q", got.Reason, rubycontroller.ReasonDepthCap)
	}
}

func itoa(prefix string, n int) string {
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
