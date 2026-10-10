// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package prefilter

import (
	"regexp"
	"strings"
	"testing"
)

// TestMatcherAgreesWithRegexpOnTargetedInputs checks the literal scanner
// against Go's own matcher on inputs chosen to break a naive ASCII
// strings.Contains check or a naive \b check.
func TestMatcherAgreesWithRegexpOnTargetedInputs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		pattern string
		input   string
	}{
		{"kelvin sign folds to k", `(?i)kelvin`, "\u212aelvin"},
		{"long s folds to s", `(?i)source`, "\u017fource"},
		{"long s inside word", `(?i)find_in_parent_folder` + `s\(`, "find_in_parent_folder\u017f("},
		{"upper case fold", `(?i)config_path`, "CONFIG_PATH = \"x\""},
		{"case sensitive rejects upper", `config_path`, "CONFIG_PATH"},
		{"boundary before holds at start", `\bsource`, "source"},
		{"boundary before fails inside word", `\bsource`, "resource"},
		{"boundary before fails after digit", `\bsource`, "9source"},
		{"boundary before holds after non-ASCII letter", `\bsource`, "\u00e9source"},
		{"boundary after holds at end", `source\b`, "a source"},
		{"boundary after fails before underscore", `source\b`, "source_x"},
		{"boundary after holds before non-ASCII letter", `source\b`, "source\u00e9"},
		{"long s then boundary", `(?i)\bsource\b`, "x \u017fource y"},
		{"long s preceded by word char fails boundary", `(?i)\bsource\b`, "x\u017fource"},
		{"nul bytes", `(?i)a_b`, "\x00A_B\x00"},
		{"crlf", `(?im)^\s*config_path`, "a\r\nCONFIG_PATH = 1\r\n"},
		{"invalid utf8 prefix", `(?i)source`, "\xff\xfesource"},
		{"truncated multi byte before literal", `(?i)source`, "\xe2\x82source"},
		{"truncated long s", `(?i)source`, "\xc5ource"},
		{"empty input", `(?i)source`, ""},
		{"literal at very end", `(?i)source`, strings.Repeat("a", 4096) + "SOURCE"},
		{"near miss", `(?i)source`, "sourc e sourse"},
		{"two alternatives second present", `(?i)\b(?:file|templatefile)\(`, "x = templatefile(a)"},
		{"profile does not satisfy boundary", `(?i)\b(?:file|templatefile)\(`, "profile(a)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			re := regexp.MustCompile(tc.pattern)
			set := NewSet(re)
			gate := set.Evaluate(tc.input)[0]
			match := re.MatchString(tc.input)
			if match && !gate {
				t.Fatalf("UNSOUND: %q matches %q but the gate (%s) rejects it", tc.pattern, tc.input, set.Describe(0))
			}
		})
	}
}

// TestMatcherRejectsTextWithoutTheLiteral proves the gate is selective, not
// merely sound: a prefilter that always says true would pass every test above.
func TestMatcherRejectsTextWithoutTheLiteral(t *testing.T) {
	t.Parallel()
	cases := []struct {
		pattern string
		input   string
	}{
		{`(?i)config_path`, "locals { a = 1 }"},
		{`(?i)\bsource\b`, "resource \"aws_s3_bucket\" \"b\" {}"},
		{`(?i)\b(?:file|templatefile)\(`, "profile(x) filename(y)"},
		{`(?i)find_in_parent_folders\(\s*\)`, "find_in_parent_folders"},
		{`foo`, "FOO"},
	}
	for _, tc := range cases {
		set := NewSet(regexp.MustCompile(tc.pattern))
		if set.Evaluate(tc.input)[0] {
			t.Errorf("gate for %q (%s) did not reject %q", tc.pattern, set.Describe(0), tc.input)
		}
	}
}

// TestUngatedExpressionAlwaysEvaluatesTrue pins the fallback: nothing
// derivable means the expression must be run.
func TestUngatedExpressionAlwaysEvaluatesTrue(t *testing.T) {
	t.Parallel()
	set := NewSet(regexp.MustCompile(`[a-z]+`), regexp.MustCompile(`\x{FFFD}`), regexp.MustCompile(``))
	for i := 0; i < set.Len(); i++ {
		if set.Gated(i) {
			t.Fatalf("expression %d must be ungated, got %s", i, set.Describe(i))
		}
	}
	for _, in := range []string{"", "x", "\xff", strings.Repeat("z", 1<<16)} {
		for i, ok := range set.Evaluate(in) {
			if !ok {
				t.Fatalf("ungated expression %d reported false for %q", i, in)
			}
		}
	}
}

// TestEvaluateIsLinearOnLargeInput guards the scan loops against quadratic
// behaviour on a pathological repetition of the literal's first byte.
func TestEvaluateIsLinearOnLargeInput(t *testing.T) {
	t.Parallel()
	set := NewSet(regexp.MustCompile(`(?i)\bsource\b`), regexp.MustCompile(`\bfoo`))
	big := strings.Repeat("ssss_", 50_000) + strings.Repeat("foo", 30_000)
	got := set.Evaluate(big)
	for i, re := range []*regexp.Regexp{regexp.MustCompile(`(?i)\bsource\b`), regexp.MustCompile(`\bfoo`)} {
		if re.MatchString(big) && !got[i] {
			t.Fatalf("expression %d unsound on large input", i)
		}
	}
}
