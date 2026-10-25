// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"strings"
	"testing"
)

const hardeningSeed = "ZZsEcReT9876"

// TestRedactMarkerRedactsSameLinePEMBlocks pins F1: a PEM block whose header,
// body, and footer share one line is replaced from the header to the footer (or
// to the end of the line when the footer is absent), and a footer-less header
// still opens the multi-line state.
func TestRedactMarkerRedactsSameLinePEMBlocks(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, in, want string }{
		{"json escaped", `k: "-----BEGIN PRIVATE KEY-----\n` + hardeningSeed + `\n-----END PRIVATE KEY-----"` + "\n", `k: "REDACTED_private_key"` + "\n"},
		{"json escaped crlf text", `k: "-----BEGIN RSA PRIVATE KEY-----\r\n` + hardeningSeed + `\r\n-----END RSA PRIVATE KEY-----\r\n"` + "\n", `k: "REDACTED_private_key\r\n"` + "\n"},
		{"inline with footer", "# -----BEGIN PRIVATE KEY----- " + hardeningSeed + " -----END PRIVATE KEY-----\n", "# REDACTED_private_key\n"},
		{"inline without footer", "# -----BEGIN PRIVATE KEY----- " + hardeningSeed + "\n", "# REDACTED_private_key\n"},
		{"indented", "    -----BEGIN RSA PRIVATE KEY----- " + hardeningSeed + " -----END RSA PRIVATE KEY-----\n", "    REDACTED_private_key\n"},
		{"lowercase", "-----begin private key----- " + hardeningSeed + " -----end private key-----\n", "REDACTED_private_key\n"},
		{"two blocks", "-----BEGIN PRIVATE KEY----- a -----END PRIVATE KEY----- mid -----BEGIN PRIVATE KEY----- b -----END PRIVATE KEY-----\n", "REDACTED_private_key mid REDACTED_private_key\n"},
		{"certificate block inline", "c: -----BEGIN CERTIFICATE----- " + hardeningSeed + " -----END CERTIFICATE-----\n", "c: REDACTED_private_key\n"},
	}
	for _, tc := range cases {
		got := redactForTest(t, tc.in)
		if got != tc.want {
			t.Errorf("%s:\n got  %q\n want %q", tc.name, got, tc.want)
		}
		if strings.Contains(got, hardeningSeed) {
			t.Errorf("%s: seed survived: %q", tc.name, got)
		}
		if again := redactForTest(t, got); again != got {
			t.Errorf("%s: not a fixed point:\n%q\n%q", tc.name, got, again)
		}
	}

	// A footer-less header opens the block: the next lines are replaced too.
	got := redactForTest(t, "# -----BEGIN PRIVATE KEY----- "+hardeningSeed+"\nbody "+hardeningSeed+"\n-----END PRIVATE KEY-----\nafter: 1\n")
	if strings.Contains(got, hardeningSeed) || !strings.HasSuffix(got, "after: 1\n") {
		t.Errorf("multi-line block after an inline header: %q", got)
	}
}

// TestRedactMarkerCRLFPEMBlocks keeps CRLF line endings and redacts every line.
func TestRedactMarkerCRLFPEMBlocks(t *testing.T) {
	t.Parallel()

	in := "a: 1\r\n-----BEGIN PRIVATE KEY-----\r\n" + hardeningSeed + "\r\n-----END PRIVATE KEY-----\r\nb: 2\r\n"
	got := redactForTest(t, in)
	if strings.Contains(got, hardeningSeed) || !strings.HasPrefix(got, "a: 1\r\n") || !strings.HasSuffix(got, "b: 2\r\n") {
		t.Fatalf("got %q", got)
	}
	if strings.Count(got, "\r\n") != strings.Count(in, "\r\n") {
		t.Fatalf("line endings changed: %q", got)
	}
}

// TestRedactMarkerNonPrivateKeyPEMIsAFixedPoint pins the fuzz finding: a
// non-private-key BEGIN header used to stay in the output, so a second pass
// opened a block and redacted to the end of the file.
func TestRedactMarkerNonPrivateKeyPEMIsAFixedPoint(t *testing.T) {
	t.Parallel()

	in := "-----BEGIN 0-----\n-----END 0-----\n00\n"
	once := redactForTest(t, in)
	if twice := redactForTest(t, once); once != twice {
		t.Fatalf("not a fixed point:\n%q\n%q", once, twice)
	}
	if !strings.HasSuffix(once, "00\n") {
		t.Fatalf("the line after the block must survive: %q", once)
	}
}

// TestRedactMarkerUnicodeSpaceSeparators pins F3: Postgres [[:space:]] matches
// U+2003 and U+3000 and Go RE2's does not, so a line migration 138 flags was
// left unredacted. Both the detector and the listed-key separator accept them.
func TestRedactMarkerUnicodeSpaceSeparators(t *testing.T) {
	t.Parallel()

	for _, space := range []string{"\u2003", "\u3000", "\u00a0", "\u2028", "\u0085"} {
		for _, in := range []string{
			"password" + space + ": abcdefghij\n",
			"password:" + space + "abcdefghij\n",
			"# api_key" + space + "=" + space + "abcdefghij end\n",
			"npmAuthToken:" + space + "abcdefghij\n",
			"npmAuthToken" + space + "= abc\n",
		} {
			got := redactForTest(t, in)
			if strings.Contains(got, "abcdef") || strings.Contains(got, "= abc") {
				t.Errorf("space %U: value survived: %q -> %q", []rune(space)[0], in, got)
			}
		}
	}
}

// TestRedactMarkerFoldSensitiveRunes pins the prefilter fix: Go (?i) folds
// U+017F to s and U+212A to k, strings.ToLower does not, so a lowercase-only
// prefilter skipped lines the regular expressions match.
func TestRedactMarkerFoldSensitiveRunes(t *testing.T) {
	t.Parallel()

	for _, in := range []string{
		"\u017fecret: abcdefgh12345\n",
		"api_\u212aey=abcdefgh12345\n",
		"to\u212aen: abcdefgh12345\n",
		"pa\u017fsword: abcdefgh12345\n",
	} {
		got := redactForTest(t, in)
		if strings.Contains(got, "abcdefgh12345") {
			t.Errorf("fold-sensitive line kept its value: %q -> %q", in, got)
		}
	}
	// The listed-key prefilter folds the same way.
	if got := redactForTest(t, "npmAuthTo\u212aen: abc\n"); strings.Contains(got, "abc\n") {
		t.Errorf("listed key with a fold-sensitive rune survived: %q", got)
	}
}

func TestRedactMarkerFailsClosedOnNotTextBodies(t *testing.T) {
	t.Parallel()

	utf16 := "p\x00a\x00s\x00s\x00w\x00o\x00r\x00d\x00:\x00 \x00a\x00b\x00c\x00d\x00e\x00f\x00g\x00h\x00\n\x00"
	for name, body := range map[string]string{
		"utf-16 without a BOM": utf16,
		"nul mid-line":         "password:\x00 abcdefgh12345\n",
		"nul at the end":       "npmAuthToken: abc\x00",
		"invalid utf-8":        "npmAuthToken: \xff\n",
	} {
		if _, _, err := redactMarker(body, testCredentialKeys); err == nil {
			t.Errorf("%s: redactMarker accepted a body that is not text", name)
		}
	}
}

func TestRedactMarkerRejectsOversizedBodies(t *testing.T) {
	t.Parallel()

	atCap := strings.Repeat("a", MaxGovernedBodyBytes)
	if _, _, err := redactMarker(atCap, testCredentialKeys); err != nil {
		t.Fatalf("a body of exactly the cap must be accepted: %v", err)
	}
	if _, _, err := redactMarker(atCap+"a", testCredentialKeys); err == nil {
		t.Fatal("a body over the cap must fail closed, never run the redactor unbounded")
	}
}

// TestRedactMarkerKnownLimits pins the denylist's documented fail-open shapes
// (README "Known limits"). They need a listed key spelled in a way a
// line-wise matcher cannot see, so they are out of reach for the closed-schema
// files marker mode is for. A future fix flips these assertions on purpose.
func TestRedactMarkerKnownLimits(t *testing.T) {
	t.Parallel()

	for name, in := range map[string]string{
		"key split across lines in invalid YAML": "\"npmAuth\\\n  Ident\": " + hardeningSeed + "\n",
		"homoglyph key":                          "npmAuthTоken: " + hardeningSeed + "\n",
		"anchor value on a key":                  "base: &t " + hardeningSeed + "\n",
		"unlisted key with text":                 "note: " + hardeningSeed + "\n",
	} {
		if got := redactForTest(t, in); got != in {
			t.Errorf("%s: the documented limit no longer holds (good news, update the README): %q -> %q", name, in, got)
		}
	}
}

// TestRedactMarkerHonorsAPEMHeaderInEveryBranch pins the delta-review finding
// DR1: redactText returned from the in-block, open-quote, and continuation
// branches before the PEM pass, so a footer-less header swallowed there never
// opened the PEM state and a flat key body after it survived. Every layout
// below puts a footer-less BEGIN header on a line handled by a different branch,
// then a flat body, a footer, and a sibling key that must survive.
func TestRedactMarkerHonorsAPEMHeaderInEveryBranch(t *testing.T) {
	t.Parallel()

	const tail = "BODYSEED\n-----END PRIVATE KEY-----\nafter: 1\n"
	cases := []struct{ name, in string }{
		{"continuation after a plain scalar", "npmAuthToken: x\n  -----BEGIN PRIVATE KEY-----\n" + tail},
		{"continuation after a detector key", "password: abcdefgh\n -----BEGIN PRIVATE KEY-----\n" + tail},
		{"open quote closing on the header line", "npmAuthToken: \"a\nb\" -----BEGIN PRIVATE KEY-----\n" + tail},
		{"block scalar line", "npmAuthToken: |\n  -----BEGIN PRIVATE KEY-----\n" + tail},
		{"sequence item", "npmAuthToken:\n- -----BEGIN PRIVATE KEY-----\n" + tail},
		{"inside a block, a new header after a footer", "-----BEGIN PRIVATE KEY-----\nZZ\n-----END PRIVATE KEY----- -----BEGIN PRIVATE KEY-----\n" + tail},
		{"comment line", "# -----BEGIN PRIVATE KEY-----\n" + tail},
		{"listed key and header on one line", "npmAuthToken: -----BEGIN PRIVATE KEY-----\n" + tail},
		{"detector key and header on one line", "password: -----BEGIN PRIVATE KEY-----\n" + tail},
		{"marker kept, then a header", "npmAuthToken: ${NPM_TOKEN}\n  -----BEGIN PRIVATE KEY-----\n" + tail},
		{"already-redacted key line, then a header", "npmAuthToken: REDACTED_api_token\n  -----BEGIN PRIVATE KEY-----\n" + tail},
		{"non-private header after a continuation", "npmAuthToken: x\n  -----BEGIN CERTIFICATE-----\n" + tail},
		{"header after a blank continuation line", "npmAuthToken:\n\n  -----BEGIN PRIVATE KEY-----\n" + tail},
	}
	for _, tc := range cases {
		got := redactForTest(t, tc.in)
		if strings.Contains(got, "BODYSEED") {
			t.Errorf("%s: the key body survived:\n%q\n->\n%q", tc.name, tc.in, got)
		}
		if !strings.HasSuffix(got, "after: 1\n") {
			t.Errorf("%s: the sibling key after the footer must survive: %q", tc.name, got)
		}
		if again := redactForTest(t, got); again != got {
			t.Errorf("%s: not a fixed point:\n%q\n%q", tc.name, got, again)
		}
		if strings.Count(got, "\n") != strings.Count(tc.in, "\n") {
			t.Errorf("%s: line count changed", tc.name)
		}
	}
}
