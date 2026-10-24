// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"strings"
	"testing"
)

const tailSeed = "ZZsEcReT9876"

// TestRedactMarkerRedactsDetectorKeyMatchToEndOfLine pins arbiter ruling B1
// (2026-10-24): a detector key match on a line without a listed key applies the
// listed-key rule, so everything from the separator to the end of the line is
// replaced and the credential tail (a Bearer scheme word, text after a comma, a
// trailing comment) cannot survive.
func TestRedactMarkerRedactsDetectorKeyMatchToEndOfLine(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, in, want string }{
		{"bearer", "Authorization: Bearer " + tailSeed + "\n", "Authorization: REDACTED_secret_literal\n"},
		{"comma tail", "db_password=abcdef,tail" + tailSeed + "\n", "db_password=REDACTED_password_literal\n"},
		{"quoted value with trailing comment", "password: \"abcdef\" # old: " + tailSeed + "\n", "password: \"REDACTED_password_literal\"\n"},
		{"bearer env reference is not exact", "Authorization: Bearer ${TOKEN}\n", "Authorization: REDACTED_secret_literal\n"},
		{"exact env reference is not a detector match", "token: ${TOKEN}\n", "token: ${TOKEN}\n"},
		{"free text comment", "# the api_key=abcdef123456 was pasted " + tailSeed + "\n", "# the api_key=REDACTED_api_token\n"},
		{"single quote", "client_secret = 'abcdef123456' " + tailSeed + "\n", "client_secret = 'REDACTED_secret_literal'\n"},
		{"keeps text before the key", "x: y; password: abcdef123456 z\n", "x: y; password: REDACTED_password_literal\n"},
	}
	for _, tc := range cases {
		got := redactForTest(t, tc.in)
		if got != tc.want {
			t.Errorf("%s:\n got  %q\n want %q", tc.name, got, tc.want)
		}
		if strings.Contains(got, tailSeed) {
			t.Errorf("%s: the tail survived: %q", tc.name, got)
		}
		if again := redactForTest(t, got); again != got {
			t.Errorf("%s: not a fixed point:\n%q\n%q", tc.name, got, again)
		}
	}
}

// TestRedactMarkerKeyMatchOpensTheContinuation proves a detector key match
// behaves as a listed key for the lines that follow it.
func TestRedactMarkerKeyMatchOpensTheContinuation(t *testing.T) {
	t.Parallel()

	got := redactForTest(t, "password: \"abcdef\n  "+tailSeed+" more\"\nnext: ok\n")
	if strings.Contains(got, tailSeed) || !strings.HasSuffix(got, "next: ok\n") {
		t.Fatalf("got %q", got)
	}
}

// TestRedactMarkerRedactsDetectorShapeMatchToEndOfLine pins B1 for the
// value-shape matches: from the shape to the end of the line.
func TestRedactMarkerRedactsDetectorShapeMatchToEndOfLine(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, in, want string }{
		{"aws key", "# rotate AKIAABCDEFGHIJKLMNOP soon " + tailSeed + "\n", "# rotate REDACTED_aws_access_key\n"},
		{"slack token", "x: xoxb-0123456789ab " + tailSeed + "\n", "x: REDACTED_slack_token\n"},
		{"stripe key defaults the kind", "stripe: sk_live_abcdefgh12345678 " + tailSeed + "\n", "stripe: REDACTED_secret_literal\n"},
		{"shape left of a key wins", "AKIAABCDEFGHIJKLMNOP then password: abcdef123456 " + tailSeed + "\n", "REDACTED_aws_access_key\n"},
	}
	for _, tc := range cases {
		got := redactForTest(t, tc.in)
		if got != tc.want {
			t.Errorf("%s:\n got  %q\n want %q", tc.name, got, tc.want)
		}
		if again := redactForTest(t, got); again != got {
			t.Errorf("%s: not a fixed point:\n%q\n%q", tc.name, got, again)
		}
	}
}
