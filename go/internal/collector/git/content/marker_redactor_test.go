// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/secret/detect"
)

var testCredentialKeys = []string{"npmAuthToken", "npmAuthIdent"}

func redactForTest(t *testing.T, body string, keys ...string) string {
	t.Helper()
	if len(keys) == 0 {
		keys = testCredentialKeys
	}
	got, _, err := redactMarker(body, keys)
	if err != nil {
		t.Fatalf("redactMarker(%q): %v", body, err)
	}
	return got
}

// detectedKind mirrors migration 138 on one stored line: the line gate
// (SQLPattern) then the CASE. It returns "" when the line is not a finding.
func detectedKind(line string) string {
	if len(detect.Spans(line)) == 0 {
		return ""
	}
	return detect.Classify(line)
}

func TestRedactMarkerReplacesListedKeyValuesAtAnyLength(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, in, want string }{
		{"literal token", "npmAuthToken: abcdef123456\n", "npmAuthToken: REDACTED_api_token\n"},
		{"one char token", "npmAuthToken: a\n", "npmAuthToken: REDACTED_api_token\n"},
		{"five char token", "npmAuthToken: abcde\n", "npmAuthToken: REDACTED_api_token\n"},
		{"quoted token", "npmAuthToken: \"abc\"\n", "npmAuthToken: \"REDACTED_api_token\"\n"},
		{"single quoted token", "  npmAuthToken: 'abc'\n", "  npmAuthToken: 'REDACTED_api_token'\n"},
		{"ident has no detector arm", "npmAuthIdent: dXNlcjpwYXNz\n", "npmAuthIdent: REDACTED_secret_literal\n"},
		{"json quoted key", "\"npmAuthToken\": \"abc\"\n", "\"npmAuthToken\": \"REDACTED_api_token\"\n"},
		{"npmrc style key", "//reg.example.com/:npmAuthToken=abc\n", "//reg.example.com/:npmAuthToken=REDACTED_api_token\n"},
		{"key is case insensitive", "NPMAUTHTOKEN: abc\n", "NPMAUTHTOKEN: REDACTED_api_token\n"},
		{"nested scope", "npmScopes:\n  acme:\n    npmAuthToken: abc\n", "npmScopes:\n  acme:\n    npmAuthToken: REDACTED_api_token\n"},
		{"crlf survives", "npmAuthToken: abc\r\nnpmRegistryServer: \"https://registry.example.com\"\r\n", "npmAuthToken: REDACTED_api_token\r\nnpmRegistryServer: \"https://registry.example.com\"\r\n"},
		{"registry url kept", "npmRegistryServer: \"https://registry.example.com/\"\nnodeLinker: node-modules\n", "npmRegistryServer: \"https://registry.example.com/\"\nnodeLinker: node-modules\n"},
		{"misplaced AKIA value keeps its kind", "npmAuthToken: AKIAABCDEFGHIJKLMNOP\n", "npmAuthToken: REDACTED_aws_access_key\n"},
	}
	for _, tc := range cases {
		if got := redactForTest(t, tc.in); got != tc.want {
			t.Errorf("%s:\n got  %q\n want %q", tc.name, got, tc.want)
		}
	}
}

func TestRedactMarkerKeepsOnlyExactEnvReferences(t *testing.T) {
	t.Parallel()

	keep := []string{
		"npmAuthToken: ${NPM_TOKEN}\n",
		"npmAuthToken: \"${NPM_TOKEN}\"\n",
		"npmAuthToken: '${NPM_TOKEN}'\n",
		"//reg.example.com/:_authToken=${NPM_TOKEN}\n",
	}
	for _, in := range keep {
		if got := redactForTest(t, in); got != in {
			t.Errorf("exact reference changed: %q -> %q", in, got)
		}
	}
	redact := []string{
		"npmAuthToken: ${NPM_TOKEN:-literalfallback}\n",
		"npmAuthToken: \"${NPM_TOKEN:-lit}\"\n",
		"npmAuthToken: prefix${NPM_TOKEN}\n",
		"npmAuthToken: ${NPM_TOKEN}suffix\n",
		"npmAuthToken: $NPM_TOKEN\n",
		// The whole remainder must be the reference: a trailing comment can
		// hold a secret, so it is redacted with the value.
		"npmAuthToken: ${NPM_TOKEN} # leak\n",
		"npmAuthToken: \"${NPM_TOKEN}\" # leak\n",
	}
	for _, in := range redact {
		got := redactForTest(t, in)
		if got == in || !strings.Contains(got, "REDACTED_api_token") {
			t.Errorf("non-exact reference kept: %q -> %q", in, got)
		}
	}
}

func TestRedactMarkerRedactsDetectorMatchesOnOtherLines(t *testing.T) {
	t.Parallel()

	cases := []struct{ in, want string }{
		{"# the api_key=abcdef123456 was pasted\n", "# the api_key=REDACTED_api_token was pasted\n"},
		{"# rotate AKIAABCDEFGHIJKLMNOP soon\n", "# rotate REDACTED_aws_access_key soon\n"},
		// sk_live_ has a shape branch in the line gate but no CASE arm, so the
		// span takes the default kind.
		{"stripe: sk_live_abcdefgh12345678\n", "stripe: REDACTED_secret_literal\n"},
	}
	for _, tc := range cases {
		if got := redactForTest(t, tc.in); got != tc.want {
			t.Errorf("detector line:\n got  %q\n want %q", got, tc.want)
		}
	}
}

func TestRedactMarkerRedactsWholePEMBlocks(t *testing.T) {
	t.Parallel()

	in := "name: x\n# -----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA\nabcdef==\n-----END RSA PRIVATE KEY-----\nafter: 1\n"
	got := redactForTest(t, in)
	for _, leaked := range []string{"MIIEow", "abcdef==", "END RSA"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("PEM body line %q survived: %q", leaked, got)
		}
	}
	if !strings.HasPrefix(got, "name: x\n") || !strings.HasSuffix(got, "after: 1\n") {
		t.Fatalf("lines outside the block changed: %q", got)
	}
	if strings.Count(got, "\n") != strings.Count(in, "\n") {
		t.Fatalf("line count changed: %q", got)
	}
}

func TestRedactMarkerRedactsUnterminatedPEMBlockToEnd(t *testing.T) {
	t.Parallel()

	in := "-----BEGIN PRIVATE KEY-----\nsecretbodyline\nlast: line\n"
	got := redactForTest(t, in)
	if strings.Contains(got, "secretbodyline") || strings.Contains(got, "last: line") {
		t.Fatalf("an unterminated PEM block must redact to end of file: %q", got)
	}
}

func TestRedactMarkerRedactsBlockScalarContinuations(t *testing.T) {
	t.Parallel()

	in := "npmAuthToken: |\n  line1secret\n  line2secret\nnpmRegistryServer: x\n"
	got := redactForTest(t, in)
	if strings.Contains(got, "secret") && !strings.Contains(got, "REDACTED_") {
		t.Fatalf("block scalar survived: %q", got)
	}
	if strings.Contains(got, "line1secret") || strings.Contains(got, "line2secret") {
		t.Fatalf("block scalar continuation survived: %q", got)
	}
	if !strings.HasSuffix(got, "npmRegistryServer: x\n") {
		t.Fatalf("the next key must survive: %q", got)
	}
}

func TestRedactMarkerIsAFixedPointAndKeepsLineCount(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		"npmAuthToken: abcdef123456",
		"npmAuthIdent: dXNlcjpwYXNz",
		"slack_token = \"xoxb-0123456789ab\"",
		"# rotate AKIAABCDEFGHIJKLMNOP soon",
		"db_password = \"abcdef123456\"",
		"client_secret = \"abcdef123456\"",
		"# -----BEGIN RSA PRIVATE KEY-----",
		"body",
		"-----END RSA PRIVATE KEY-----",
		"",
	}, "\n")
	once := redactForTest(t, body)
	twice := redactForTest(t, once)
	if once != twice {
		t.Fatalf("redaction is not a fixed point:\n once  %q\n twice %q", once, twice)
	}
	if strings.Count(once, "\n") != strings.Count(body, "\n") {
		t.Fatalf("line count changed")
	}
}

// TestRedactMarkerRejectsBlindingMarkers pins why the marker is
// REDACTED_<finding_kind>: [redacted] and *** are not findings, so a marker of
// that form would blind investigate_hardcoded_secrets.
func TestRedactMarkerRejectsBlindingMarkers(t *testing.T) {
	t.Parallel()

	for _, blind := range []string{`db_password = "[redacted]"`, `db_password = "***"`, `npmAuthToken: [redacted]`} {
		if detectedKind(blind) != "" {
			t.Errorf("%q is detected; the blinding-marker premise is wrong", blind)
		}
	}
	body := "npmAuthToken: abcdef123456\ndb_password = \"abcdef123456\"\n# secret: abcdef123456\n"
	got := redactForTest(t, body)
	for _, bad := range []string{"[redacted]", "***", "REDACTED_value"} {
		if strings.Contains(got, bad) {
			t.Errorf("marker mode wrote %q: %q", bad, got)
		}
	}
	for _, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
		if detectedKind(line) == "" {
			t.Errorf("redacted line is no longer a finding under migration 138: %q", line)
		}
	}
}

// TestRedactMarkerPerKindResultUnderMigration138 is the per-kind table for the
// six finding kinds (a key line and a free-text line each). It pins the
// MEASURED result of migration 138 on the marked line: survives (same kind),
// lost (not detected), or kind changed. #8833's marker arm updates every row to
// "same kind". The probe lines and results are the evidence page's marker probe.
func TestRedactMarkerPerKindResultUnderMigration138(t *testing.T) {
	t.Parallel()

	const (
		survives = "survives"
		lost     = "lost"
	)
	cases := []struct {
		kind, shape, in, wantMarked string
		after                       string // detected kind on the marked line under migration 138
	}{
		{"aws_access_key", "key", `access_key_id = "AKIAABCDEFGHIJKLMNOP"`, `access_key_id = "REDACTED_aws_access_key"`, lost},
		{"aws_access_key", "free", `# rotate AKIAABCDEFGHIJKLMNOP soon`, `# rotate REDACTED_aws_access_key soon`, lost},
		{"slack_token", "key", `slack_webhook = "xoxb-0123456789ab"`, `slack_webhook = "REDACTED_slack_token"`, lost},
		{"slack_token", "free", `# post with xoxb-0123456789ab here`, `# post with REDACTED_slack_token here`, lost},
		{"private_key", "key", `key = "-----BEGIN RSA PRIVATE KEY-----"`, `key = "REDACTED_private_key"`, lost},
		{"private_key", "free", `# -----BEGIN RSA PRIVATE KEY-----`, `# REDACTED_private_key`, lost},
		{"api_token", "key", `npmAuthToken: abcdef123456`, `npmAuthToken: REDACTED_api_token`, "api_token"},
		{"api_token", "free", `# the api_key=abcdef123456 was pasted`, `# the api_key=REDACTED_api_token was pasted`, "api_token"},
		{"password_literal", "key", `db_password = "abcdef123456"`, `db_password = "REDACTED_password_literal"`, "password_literal"},
		{"password_literal", "free", `# password: abcdef123456 per ops`, `# password: REDACTED_password_literal per ops`, "password_literal"},
		{"secret_literal", "key", `client_secret = "abcdef123456"`, `client_secret = "REDACTED_secret_literal"`, "secret_literal"},
		{"secret_literal", "free", `# the client_secret = abcdef123456 shared`, `# the client_secret = REDACTED_secret_literal shared`, "secret_literal"},
	}
	seen := map[string]int{}
	for _, tc := range cases {
		seen[tc.kind]++
		if raw := detectedKind(tc.in); raw != tc.kind {
			t.Errorf("%s/%s: raw line detected as %q, want %q", tc.kind, tc.shape, raw, tc.kind)
		}
		marked := strings.TrimSuffix(redactForTest(t, tc.in+"\n"), "\n")
		if marked != tc.wantMarked {
			t.Errorf("%s/%s: marked %q, want %q", tc.kind, tc.shape, marked, tc.wantMarked)
		}
		got := detectedKind(marked)
		want := tc.after
		if want == lost {
			want = ""
		}
		if got != want {
			t.Errorf("%s/%s: marked line detected as %q under migration 138, want %q (%s)", tc.kind, tc.shape, got, want, tc.after)
		}
	}
	for _, kind := range detect.Kinds() {
		if seen[kind] != 2 {
			t.Errorf("kind %s has %d probe lines, want a key line and a free-text line", kind, seen[kind])
		}
	}
}

// TestRedactMarkerKindChangesUnderMigration138 pins the two measured kind
// changes: a shape marker under a listed key is detected as api_token.
func TestRedactMarkerKindChangesUnderMigration138(t *testing.T) {
	t.Parallel()

	got := redactForTest(t, "npmAuthToken: AKIAABCDEFGHIJKLMNOP\n", "npmAuthToken")
	if got != "npmAuthToken: REDACTED_aws_access_key\n" {
		t.Fatalf("got %q", got)
	}
	if kind := detectedKind(strings.TrimSuffix(got, "\n")); kind != "api_token" {
		t.Fatalf("migration 138 detects %q, want the measured api_token (raw kind was aws_access_key)", kind)
	}
	slack := redactForTest(t, "slack_token = \"xoxb-0123456789ab\"\n")
	if kind := detectedKind(strings.TrimSuffix(slack, "\n")); kind != "api_token" {
		t.Fatalf("slack_token key line detected as %q, want api_token", kind)
	}
}

func TestRedactMarkerRejectsInvalidUTF8(t *testing.T) {
	t.Parallel()

	if _, _, err := redactMarker("npmAuthToken: \xff\xfe\n", testCredentialKeys); err == nil {
		t.Fatal("invalid UTF-8 must be an error, never a silent pass-through")
	}
}

// TestRedactMarkerFailsClosedOnYAMLValueShapes pins the shapes where a listed
// key's value is not on the key line: block scalars with a comment, tag, or
// anchor, an empty value, a same-indent sequence, a multi-line plain scalar,
// and a quoted scalar that closes on a later line. Every continuation line is
// redacted; the next sibling key survives.
func TestRedactMarkerFailsClosedOnYAMLValueShapes(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, in string }{
		{"block scalar with comment", "npmAuthToken: | # token\n  leak1\n  leak2\nnext: ok\n"},
		{"block scalar with chomping and indent", "npmAuthToken: |2-\n    leak1\n    leak2\nnext: ok\n"},
		{"folded scalar", "npmAuthToken: >\n  leak1\n  leak2\nnext: ok\n"},
		{"tagged block scalar", "npmAuthToken: !!str |\n  leak1\n  leak2\nnext: ok\n"},
		{"anchored empty value", "npmAuthToken: &tok\n  leak1\n  leak2\nnext: ok\n"},
		{"empty value then nested lines", "npmAuthToken:\n  leak1\n  leak2\nnext: ok\n"},
		{"comment value then nested lines", "npmAuthToken: # see below\n  leak1\n  leak2\nnext: ok\n"},
		{"same indent sequence", "npmAuthToken:\n- leak1\n- leak2\nnext: ok\n"},
		{"multi-line plain scalar", "npmAuthToken: leak1\n  leak2\nnext: ok\n"},
		{"multi-line double quoted", "npmAuthToken: \"leak1\n  leak2\"\nnext: ok\n"},
		{"multi-line single quoted", "npmAuthToken: 'leak1\n  leak2'\nnext: ok\n"},
		{"escaped quote does not close", "npmAuthToken: \"leak1 \\\" still leak\n  leak2\"\nnext: ok\n"},
		{"doubled single quote does not close", "npmAuthToken: 'leak1 '' still leak\n  leak2'\nnext: ok\n"},
	}
	for _, tc := range cases {
		got := redactForTest(t, tc.in)
		if strings.Contains(got, "leak") {
			t.Errorf("%s: a value line survived:\n%s", tc.name, got)
		}
		if !strings.HasSuffix(got, "next: ok\n") {
			t.Errorf("%s: the next sibling key must survive:\n%s", tc.name, got)
		}
		if strings.Count(got, "\n") != strings.Count(tc.in, "\n") {
			t.Errorf("%s: line count changed", tc.name)
		}
		if again := redactForTest(t, got); again != got {
			t.Errorf("%s: not a fixed point:\n%s\n--\n%s", tc.name, got, again)
		}
	}
}

func TestRedactMarkerKeepsLinesOutsideTheValue(t *testing.T) {
	t.Parallel()

	in := "# header\nnpmAuthToken: abc\nnpmRegistryServer: \"https://registry.example.com/\"\nenableTelemetry: false\n"
	want := "# header\nnpmAuthToken: REDACTED_api_token\nnpmRegistryServer: \"https://registry.example.com/\"\nenableTelemetry: false\n"
	if got := redactForTest(t, in); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRedactMarkerRedactsValuesThatLookLikeYAMLSyntax pins the fail-closed
// reading of "#", "!", and "&": after "=" they are value characters, a lone
// tag or anchor token is a value, and a "#" with no space before it is not a
// YAML comment.
func TestRedactMarkerRedactsValuesThatLookLikeYAMLSyntax(t *testing.T) {
	t.Parallel()

	for _, in := range []string{
		"//reg.example.com/:_authToken=#leak123\n",
		"//reg.example.com/:_authToken=!leak123\n",
		"//reg.example.com/:_authToken=&leak123\n",
		"npmAuthToken=#leak123\n",
		"npmAuthToken:#leak123\n",
		"npmAuthToken: !leak123\n",
		"npmAuthToken: &leak123\n",
		"npmAuthToken: !!str &leak123\n",
		"npmAuthToken: !leak123 # c\n",
	} {
		got := redactForTest(t, in, "npmAuthToken", "_authToken")
		if strings.Contains(got, "leak123") {
			t.Errorf("value survived: %q -> %q", in, got)
		}
		if !strings.Contains(got, "REDACTED_") {
			t.Errorf("no marker written: %q -> %q", in, got)
		}
	}
	// A YAML comment after a spaced colon is not a value.
	if got := redactForTest(t, "npmAuthToken: # see below\nnext: ok\n"); got != "npmAuthToken: # see below\nnext: ok\n" {
		t.Errorf("a comment-only value changed: %q", got)
	}
}
