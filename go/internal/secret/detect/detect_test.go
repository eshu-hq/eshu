// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package detect_test

import (
	"regexp"
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

	const want = `(password|passwd|pwd|api[_-]?key|apikey|token|secret|client[_-]?secret|private[_-]?key|authorization)[[:space:]]*[:=][[:space:]]*['"]?[A-Za-z0-9_./+=:@!#$%^-]{6,}|AKIA[0-9A-Z]{16}|sk_live_[A-Za-z0-9]{8,}|xox[baprs]-[A-Za-z0-9-]{10,}|-----BEGIN [A-Z ]*PRIVATE KEY-----`
	if detect.SQLPattern != want {
		t.Fatalf("SQLPattern = %q, want the migration 138 pattern", detect.SQLPattern)
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
