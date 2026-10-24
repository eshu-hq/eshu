// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package detect_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/secret/detect"
)

func TestKindsListTheSixFindingKindsInCaseOrder(t *testing.T) {
	t.Parallel()

	want := []string{"aws_access_key", "private_key", "slack_token", "api_token", "password_literal", "secret_literal"}
	got := detect.Kinds()
	if len(got) != len(want) {
		t.Fatalf("Kinds() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Kinds()[%d] = %q, want %q (migration 138 CASE order)", i, got[i], want[i])
		}
	}
}

func TestClassifyUsesTheFirstMatchingCaseArm(t *testing.T) {
	t.Parallel()

	cases := []struct {
		line string
		want string
	}{
		{"access_key_id = \"AKIAABCDEFGHIJKLMNOP\"", "aws_access_key"},
		{"# rotate AKIAABCDEFGHIJKLMNOP soon", "aws_access_key"},
		{"key = \"-----BEGIN RSA PRIVATE KEY-----\"", "private_key"},
		{"slack_webhook = \"xoxb-0123456789ab\"", "slack_token"},
		{"npmAuthToken: abcdef123456", "api_token"},
		{"# the api_key=abcdef123456 was pasted", "api_token"},
		{"db_password = \"abcdef123456\"", "password_literal"},
		{"client_secret = \"abcdef123456\"", "secret_literal"},
		// The value-shape arms come before the key arms.
		{"api_token: AKIAABCDEFGHIJKLMNOP", "aws_access_key"},
		{"nothing to see", ""},
	}
	for _, tc := range cases {
		if got := detect.Classify(tc.line); got != tc.want {
			t.Errorf("Classify(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
}

func TestSQLPatternIsTheComposedKeyAndShapeBranches(t *testing.T) {
	t.Parallel()

	const want = `(password|passwd|pwd|api[_-]?key|apikey|token|secret|client[_-]?secret|private[_-]?key|authorization|npm[_-]?auth[_-]?ident)['"]?[[:space:]]*[:=][[:space:]]*['"]?[A-Za-z0-9_./+=:@!#$%^-]{6,}` +
		`|AKIA[0-9A-Z]{16}|sk_live_[A-Za-z0-9]{8,}|xox[baprs]-[A-Za-z0-9-]{10,}|-----BEGIN [A-Z ]*PRIVATE KEY-----` +
		`|//[^/?#@[:space:]:]+:[^/?#@[:space:]]+@` +
		`|(password|passwd|pwd|api[_-]?key|apikey|token|secret|credential)s?[[:space:]]*=[[:space:]]*[{][[:space:]]*$`
	if detect.SQLPattern != want {
		t.Fatalf("SQLPattern = %q, want the migration 234 pattern", detect.SQLPattern)
	}
	if detect.MarkerPrefix != "REDACTED_" {
		t.Fatalf("MarkerPrefix = %q", detect.MarkerPrefix)
	}
	// MustCompile panics if the Postgres pattern is not valid RE2.
	_ = regexp.MustCompile("(?i)" + detect.SQLPattern)
}

func TestValueSpansReturnsOnlyTheValueOfKeyMatches(t *testing.T) {
	t.Parallel()

	line := "# the api_key=abcdef123456 was pasted AKIAABCDEFGHIJKLMNOP"
	spans := detect.Spans(line)
	if len(spans) != 2 {
		t.Fatalf("Spans(%q) = %v, want 2 spans", line, spans)
	}
	if got := line[spans[0].Start:spans[0].End]; got != "abcdef123456" {
		t.Errorf("key span = %q, want only the value", got)
	}
	if got := line[spans[1].Start:spans[1].End]; got != "AKIAABCDEFGHIJKLMNOP" {
		t.Errorf("shape span = %q, want the whole shape", got)
	}
}

// TestSpansPrefilterNeverHidesAMatch proves the lowercase-word prefilter is a
// pure speedup: for every line shape the pattern matches, Spans finds it, and
// for a corpus of clean lines it finds nothing.
func TestSpansPrefilterNeverHidesAMatch(t *testing.T) {
	t.Parallel()

	matching := []string{
		"password: abcdef", "PASSWD=abcdef", "pwd = 'abcdef'", "API_KEY=abcdef", "api-key: abcdef", "apikey=abcdef",
		"Token: abcdef", "SECRET=abcdef", "client_secret: abcdef", "client-secret=abcdef", "private_key=abcdef",
		"private-key: abcdef", "Authorization: abcdef", "x AKIAABCDEFGHIJKLMNOP y", "sk_live_abcdefgh", "xoxb-0123456789ab",
		"-----BEGIN RSA PRIVATE KEY-----", "-----begin private key-----",
	}
	for _, line := range matching {
		full := regexp.MustCompile("(?i)" + detect.SQLPattern)
		if full.FindString(line) == "" {
			t.Fatalf("test premise: %q should match the pattern", line)
		}
		if len(detect.Spans(line)) == 0 {
			t.Errorf("Spans(%q) found nothing, but the pattern matches", line)
		}
	}
	for _, line := range []string{"", "nodeLinker: node-modules", "compressionLevel: mixed", "a: 1", "# comment only"} {
		if spans := detect.Spans(line); len(spans) != 0 {
			t.Errorf("Spans(%q) = %v, want none", line, spans)
		}
	}
}

// TestSpansAndClassifyAcceptUnicodeSpaces pins F3. Postgres ARE [[:space:]]
// matches U+2003 and U+3000 (measured on 18.6) and Go RE2's does not, so the
// Go-only regular expressions use a superset class. SQLPattern keeps the
// Postgres spelling, which the binding test compares with the migration.
func TestSpansAndClassifyAcceptUnicodeSpaces(t *testing.T) {
	t.Parallel()

	for _, space := range []string{"\u2003", "\u3000", "\u00a0", "\u2028", "\u2029", "\u0085", " ", "\t", "\v", "\f"} {
		line := "password" + space + ": abcdefghij"
		if len(detect.Spans(line)) == 0 {
			t.Errorf("Spans(%q) found nothing for space %U", line, []rune(space)[0])
		}
		if got := detect.Classify(line); got != "password_literal" {
			t.Errorf("Classify(%q) = %q, want password_literal for space %U", line, got, []rune(space)[0])
		}
		after := "api_key:" + space + "abcdefghij"
		if len(detect.Spans(after)) == 0 {
			t.Errorf("Spans(%q) found nothing for space %U after the separator", after, []rune(space)[0])
		}
	}
	if !strings.Contains(detect.SQLPattern, "[[:space:]]") {
		t.Fatal("SQLPattern must keep the Postgres spelling [[:space:]]")
	}
}

// TestSpansPrefilterKeepsNonASCIILines pins the fold fix: a line with any
// non-ASCII byte skips no regular expression, because Go (?i) folds U+017F to s
// and U+212A to k and strings.ToLower does not.
func TestSpansPrefilterKeepsNonASCIILines(t *testing.T) {
	t.Parallel()

	for _, line := range []string{"\u017fecret: abcdefgh12345", "api_\u212aey=abcdefgh12345", "to\u212aen: abcdefgh12345"} {
		if len(detect.Spans(line)) == 0 {
			t.Errorf("Spans(%q) skipped a line the pattern matches", line)
		}
	}
}

// TestSpacesClassKeepsThePOSIXSetAndAddsUnicode pins the ruling's spelling: the
// class is the POSIX [[:space:]] set (which has \v, unlike Go's \s) plus Zs, NEL,
// and the line and paragraph separators, substituted at compile time only.
func TestSpaceClassKeepsThePOSIXSetAndAddsUnicode(t *testing.T) {
	t.Parallel()

	const want = `[[:space:]\p{Zs}\x{85}\x{2028}\x{2029}]`
	if detect.SpaceClass != want {
		t.Fatalf("SpaceClass = %q, want %q", detect.SpaceClass, want)
	}
	for _, source := range detect.ArmSources() {
		if strings.Contains(source.Regex, `\p{Zs}`) {
			t.Fatalf("arm %s leaked the Go-only class into the migration-bound text: %s", source.Kind, source.Regex)
		}
	}
}

// TestSpansReportKeyMatchGeometry pins the fields the redactor needs to apply
// the listed-key rule to a detector key match: where the key word is, where the
// separator and its trailing space end (before an opening quote), and where the
// value starts.
func TestSpansReportKeyMatchGeometry(t *testing.T) {
	t.Parallel()

	line := `x: Authorization: "abcdefghij" tail`
	spans := detect.Spans(line)
	if len(spans) != 1 || !spans[0].IsKey {
		t.Fatalf("Spans = %+v, want one key span", spans)
	}
	span := spans[0]
	if got := line[span.KeyStart:span.KeyEnd]; got != "Authorization" {
		t.Errorf("key = %q", got)
	}
	if got := line[span.SepEnd:span.Start]; got != `"` {
		t.Errorf("between the separator and the value = %q, want the opening quote", got)
	}
	if got := line[span.KeyEnd:span.SepEnd]; got != ": " {
		t.Errorf("separator = %q", got)
	}
	shape := detect.Spans("# rotate AKIAABCDEFGHIJKLMNOP soon")
	if len(shape) != 1 || shape[0].IsKey {
		t.Fatalf("shape span = %+v, want one non-key span", shape)
	}
}
