// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package prefilter

import (
	"regexp"
	"testing"
)

// TestDeriveDescribesRequiredLiterals pins what the derivation produces for
// each syntax shape. A fold literal is marked ~ and a word boundary \b.
func TestDeriveDescribesRequiredLiterals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		pattern string
		want    string
		gated   bool
	}{
		{"plain literal", `config_path`, `"config_path"`, true},
		{"fold literal", `(?i)config_path`, `"config_path"~`, true},
		{"escaped paren joins the literal", `(?i)find\(`, `"find("~`, true},
		{"alternation of literals", `foo|bar`, `("foo" OR "bar")`, true},
		{"alternation joined to a suffix", `(?:file|templatefile)\(`, `("file(" OR "templatefile(")`, true},
		{"required plus optional", `foo(?:bar)?baz`, `("foo" AND "baz")`, true},
		{"star requires nothing", `(?:foo)*`, `TRUE`, false},
		{"plus requires its body", `(?:foo)+bar`, `("foo" AND "bar")`, true},
		{"quest requires nothing", `foo?`, `"fo"`, true},
		{"class is not a requirement", `[a-z]+`, `TRUE`, false},
		{"small class is exact", `a[xy]b`, `("axb" OR "ayb")`, true},
		{"wildcard is not a requirement", `.*`, `TRUE`, false},
		{"empty pattern", ``, `TRUE`, false},
		{"anchors are transparent", `^foo$`, `"foo"`, true},
		{"boundary before", `\bfoo`, `\b"foo"`, true},
		{"boundary after", `foo\b`, `"foo"\b`, true},
		{"boundary both with fold", `(?i)\bsource\b`, `\b"source"~\b`, true},
		{"boundary not adjacent is dropped", `\b\s*foo`, `"foo"`, true},
		{"not word boundary is transparent", `\Bfoo`, `"foo"`, true},
		{"replacement rune is never gated", `\x{FFFD}`, `TRUE`, false},
		{"one alternative without a literal", `foo|[a-z]+`, `TRUE`, false},
		{"concat of non-exact keeps both", `foo.*bar`, `("foo" AND "bar")`, true},
		{"fold applies per literal", `a(?i:b)c`, `"abc"~`, true},
		{"too many alternatives fall back to parts", `(?:aa|bb|cc|dd|ee)(?:ff|gg|hh|ii|jj)x`, `(("ffx" OR "ggx" OR "hhx" OR "iix" OR "jjx") AND ("aa" OR "bb" OR "cc" OR "dd" OR "ee"))`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			set := NewSet(regexp.MustCompile(tc.pattern))
			if got := set.Describe(0); got != tc.want {
				t.Fatalf("Describe(%q) = %s, want %s", tc.pattern, got, tc.want)
			}
			if got := set.Gated(0); got != tc.gated {
				t.Fatalf("Gated(%q) = %v, want %v", tc.pattern, got, tc.gated)
			}
		})
	}
}

// TestDeriveSharesLiteralsAcrossExpressions proves one scan serves every
// expression that needs the same literal.
func TestDeriveSharesLiteralsAcrossExpressions(t *testing.T) {
	t.Parallel()
	set := NewSet(
		regexp.MustCompile(`(?i)find_in_parent_folders\(\s*\)`),
		regexp.MustCompile(`(?i)\bpath\s*=\s*find_in_parent_folders\(`),
	)
	if got, want := len(set.matchers), 4; got != want {
		t.Fatalf("matcher table = %d entries, want %d (shared find_in_parent_folders literal)", got, want)
	}
}

// TestDeriveFoldOrbitsIncludeSpecialFolds pins that fold literals carry the
// Unicode simple-fold orbit, not an ASCII toggle.
func TestDeriveFoldOrbitsIncludeSpecialFolds(t *testing.T) {
	t.Parallel()
	set := NewSet(regexp.MustCompile(`(?i)ks`))
	got := set.Evaluate("\u212a\u017f")
	if !got[0] {
		t.Fatal("KELVIN SIGN + LONG S must satisfy (?i)ks")
	}
	if !regexp.MustCompile(`(?i)ks`).MatchString("\u212a\u017f") {
		t.Fatal("Go regexp no longer folds KELVIN SIGN and LONG S; revisit the orbit assumption")
	}
	if set.Evaluate("kx")[0] {
		t.Fatal("kx must not satisfy (?i)ks")
	}
}

// TestDeriveEveryLiteralIsNonEmpty is a structural invariant: an empty
// literal would make matcher.boundaryHolds index out of range.
func TestDeriveEveryLiteralIsNonEmpty(t *testing.T) {
	t.Parallel()
	for _, p := range soundnessPatterns() {
		set := NewSet(regexp.MustCompile(p))
		for _, m := range set.matchers {
			if len(m.elems) == 0 {
				t.Fatalf("pattern %q derived an empty literal", p)
			}
		}
	}
}
