// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package detect_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/secret/detect"
)

// TestFindingKeepsEveryKindMarkedLine pins the #8833 marker arm: a line holding
// REDACTED_<kind> for any of the six kinds is a finding of that kind, first in
// the CASE, so a redacted line keeps its raw (line_number, finding_kind).
// REDACTED_value never matches the marker arm.
func TestFindingKeepsEveryKindMarkedLine(t *testing.T) {
	t.Parallel()

	for _, kind := range detect.Kinds() {
		for _, line := range []string{
			"key = REDACTED_" + kind,
			"# note REDACTED_" + kind + " here",
			"  REDACTED_" + kind,
			"npmAuthToken: REDACTED_" + kind,
		} {
			if got := detect.Finding(line); got != kind {
				t.Errorf("Finding(%q) = %q, want %q", line, got, kind)
			}
		}
	}
	for _, line := range []string{"REDACTED_value", "  REDACTED_value", "region = REDACTED_value", "x = REDACTED_valuable"} {
		if got := detect.Finding(line); got != "" {
			t.Errorf("Finding(%q) = %q, want no finding", line, got)
		}
	}
	// A key arm still reads a key line whose value is REDACTED_value; the
	// skeleton never writes one for a line that was not a finding.
	if got := detect.Finding("db_password = REDACTED_value"); got != "password_literal" {
		t.Errorf("Finding(db_password = REDACTED_value) = %q, want password_literal", got)
	}
}

// TestFindingAddsTheRecordedExtras pins the detector extras the #8813 gap
// table lists, each first undetected under migration 138.
func TestFindingAddsTheRecordedExtras(t *testing.T) {
	t.Parallel()

	cases := []struct{ line, want string }{
		{`npmAuthIdent: "dXNlcjpwYXNzd29yZA=="`, "secret_literal"},
		{`npm_auth_ident = "dXNlcjpwYXNzd29yZA=="`, "secret_literal"},
		{`"db_password": "hunter2hunter2"`, "password_literal"},
		{`'client_secret': 'abcdefghij'`, "secret_literal"},
		{`"api_key" : "abcdefghij"`, "api_token"},
		{`endpoint = "https://admin:hunter2@db.example.com/x"`, "password_literal"},
		{`npmRegistryServer: "https://user:pass@npm.example.com/"`, "password_literal"},
		{`"//user:pass@npm.example.com":`, "password_literal"},
		{`app_secrets = {`, "secret_literal"},
		{`  db_passwords = {  `, "password_literal"},
		{`service_tokens = {`, "api_token"},
		{`credentials = {`, "secret_literal"},
		// Migration 138 kinds stay as they were.
		{`db_password = "hunter2hunter2"`, "password_literal"},
		{`api_token: AKIAABCDEFGHIJKLMNOP`, "aws_access_key"},
		// Not findings: plural keys with a value, a JSON schema opener, a URL
		// with only a user, a comment that is not userinfo, a short value.
		{`secrets: abcdefghij`, ""},
		{`"nextPageToken": {`, ""},
		{`app_secrets = { stripe = "x" }`, ""},
		{`url = "https://user@example.com/"`, ""},
		{`// see user:pass@host`, ""},
		{`api_token = "abc"`, ""},
		{`nothing to see`, ""},
	}
	for _, tc := range cases {
		if got := detect.Finding(tc.line); got != tc.want {
			t.Errorf("Finding(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
}

// TestSpansCoverTheExtrasButNotMarkers checks the redaction spans: a userinfo
// span starts at the URL scheme, so marker mode replaces the whole credential
// URL; a marker is not a credential and yields no span of its own.
func TestSpansCoverTheExtrasButNotMarkers(t *testing.T) {
	t.Parallel()

	line := `x: "https://admin:hunter2@db.example.com/x"`
	spans := detect.Spans(line)
	if len(spans) != 1 || spans[0].IsKey {
		t.Fatalf("Spans(%q) = %+v, want one shape span", line, spans)
	}
	if got := line[spans[0].Start:]; got[:len("https://admin:hunter2@")] != "https://admin:hunter2@" {
		t.Errorf("userinfo span starts at %q, want the scheme", got)
	}
	if got := detect.Spans(`"//user:pass@npm.example.com":`); len(got) != 1 {
		t.Errorf("protocol-relative userinfo spans = %+v, want one", got)
	}
	if got := detect.Spans("  REDACTED_api_token"); len(got) != 0 {
		t.Errorf("a marker alone yields spans %+v, want none", got)
	}
	if got := detect.Spans("npmAuthIdent: abcdefghij"); len(got) != 1 || !got[0].IsKey {
		t.Errorf("npmAuthIdent spans = %+v, want one key span", got)
	}
}

// TestMarkerPatternNamesTheSixKinds pins MarkerPattern to Kinds, so the raw-rows
// gauge recognises every marker the redactors write and never REDACTED_value.
func TestMarkerPatternNamesTheSixKinds(t *testing.T) {
	t.Parallel()

	re := regexp.MustCompile(detect.MarkerPattern)
	for _, kind := range detect.Kinds() {
		if !re.MatchString("x = REDACTED_" + kind) {
			t.Errorf("MarkerPattern misses REDACTED_%s", kind)
		}
	}
	if re.MatchString("x = REDACTED_value") {
		t.Error("MarkerPattern matches REDACTED_value")
	}
	if !regexp.MustCompile("(?i)" + detect.BlockOpenerPattern).MatchString("app_secrets = {") {
		t.Error("BlockOpenerPattern misses app_secrets = {")
	}
}

// TestMarkerArmIsCaseSensitive pins ruling-2 on #8833: the marker arm matches
// the exact REDACTED_<kind> the redactors write and nothing else, so a user
// line that happens to spell a marker in another case keeps the kind its own
// key or shape gives it.
func TestMarkerArmIsCaseSensitive(t *testing.T) {
	t.Parallel()

	for line, want := range map[string]string{
		"redacted_slack_token = abcdefghij":    "api_token",
		"password: redacted_aws_access_key":    "password_literal",
		"secret = Redacted_Private_Key_backup": "secret_literal",
		"x = REDACTED_slack_token":             "slack_token",
	} {
		if got := detect.Finding(line); got != want {
			t.Errorf("Finding(%q) = %q, want %q", line, got, want)
		}
	}
	for _, arm := range detect.ArmSources() {
		marker := strings.HasPrefix(arm.Regex, detect.MarkerPrefix)
		if arm.CaseSensitive != marker {
			t.Errorf("arm %q CaseSensitive = %v, want %v (only the marker arms are case-sensitive)", arm.Regex, arm.CaseSensitive, marker)
		}
	}
}
