// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"strings"
	"testing"
)

func skeletonForTest(t *testing.T, body string) string {
	t.Helper()
	got, _, err := redactSkeleton(body)
	if err != nil {
		t.Fatalf("redactSkeleton(%q): %v", body, err)
	}
	return got
}

// TestRedactSkeletonLineRules pins the #8813 record's skeleton table one line
// shape at a time.
func TestRedactSkeletonLineRules(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, in, want string }{
		// Structural lines are kept.
		{"open brace", "{", "{"},
		{"close brace comma", "  },", "  },"},
		{"close bracket", "]", "]"},
		{"object opener", "tags = {", "tags = {"},
		{"list opener", "  zones = [", "  zones = ["},
		{"json object opener", "  \"tags\": {", "  \"tags\": {"},
		{"empty object", "labels = {}", "labels = {}"},
		{"empty list json", "\"zones\": [],", "\"zones\": [],"},
		{"blank", "   ", "   "},
		{"empty", "", ""},
		// Allowlisted values are kept.
		{"true", "enabled = true", "enabled = true"},
		{"false json", "  \"enabled\": false,", "  \"enabled\": false,"},
		{"null", "owner = null", "owner = null"},
		{"integer", "instance_count = 3", "instance_count = 3"},
		{"negative float", "offset = -0.25", "offset = -0.25"},
		{"exponent", "big = 1e10", "big = 1e10"},
		{"var reference", "subnet = var.subnet_id", "subnet = var.subnet_id"},
		{"local reference", "name = local.prefix", "name = local.prefix"},
		{"module reference", "vpc = module.vpc.id", "vpc = module.vpc.id"},
		{"data reference with index", "ami = data.aws_ami.main.ids[0]", "ami = data.aws_ami.main.ids[0]"},
		{"version", "engine_version = \"14.7\"", "engine_version = \"14.7\""},
		{"version constraint", "provider_version = \"~> 5.31.0\"", "provider_version = \"~> 5.31.0\""},
		{"version range", "required = \">= 1.2, < 2.0.0\"", "required = \">= 1.2, < 2.0.0\""},
		{"version prerelease", "chart = \"v1.4.0-rc.1\"", "chart = \"v1.4.0-rc.1\""},
		{"colon separator", "  Name: true", "  Name: true"},
		// Every other value becomes REDACTED_value.
		{"plain string", "region = \"us-east-1\"", "region = REDACTED_value"},
		{"json string comma", "  \"region\": \"us-east-1\",", "  \"region\": REDACTED_value,"},
		{"quoted number", "port = \"5432\"", "port = REDACTED_value"},
		{"version with free prerelease", "chart = \"1.0-hunter2secret\"", "chart = REDACTED_value"},
		{"interpolation", "name = \"${var.prefix}-db\"", "name = REDACTED_value"},
		{"inline object", "tags = { Name = \"x\" }", "tags = REDACTED_value"},
		{"inline list", "zones = [\"a\", \"b\"]", "zones = REDACTED_value"},
		{"trailing comment", "count = 3 # three", "count = REDACTED_value"},
		{"url userinfo", "endpoint = \"https://admin:hunter2@db.example.com/x\"", "endpoint = REDACTED_value"},
		{"uppercase reference is not a reference", "x = VAR.thing", "x = REDACTED_value"},
		// A detector-flagged key is checked first and keeps its finding kind.
		{"password literal", "db_password = \"hunter2hunter2\"", "db_password = REDACTED_password_literal"},
		{"flagged reference", "db_password = var.db_password", "db_password = REDACTED_password_literal"},
		{"flagged number", "api_token = 12345678", "api_token = REDACTED_api_token"},
		{"aws key value", "access = \"AKIAABCDEFGHIJKLMNOP\"", "access = REDACTED_aws_access_key"},
		// A detector match with no finding kind (sk_live_ alone) is no finding,
		// so it is a plain REDACTED_value and the marker arm adds nothing.
		{"stripe value", "stripe = \"sk_live_abcdefgh12345678\"", "stripe = REDACTED_value"},
		// A detector match with no kind still blocks the allowlist: a
		// reference-shaped value holding a token is not provably safe (rev-8833 F1).
		{"stripe in a module reference", "x = module.sk_" + "live_4eC39HqLyjWDarjtT1zdp7dc", "x = REDACTED_value"},
		{"stripe in a var reference", "x = var.sk_" + "live_4eC39HqLyjWDarjtT1zdp7dc", "x = REDACTED_value"},
		{"stripe in a data reference", "ami = data.sk_" + "live_4eC39HqLyjWDarjtT1zdp7dc.id", "ami = REDACTED_value"},
		// A value that starts with a separator is no assignment: base64 padding
		// on a bare token reads as key "TOKEN", "=", "=" (rev-8833 F2).
		{"bare base64 token", "dGhpc2lzYXNlY3JldHRva2Vu==", "REDACTED_value"},
		{"bare base64 token in a list", "  X==,", "  REDACTED_value"},
		{"double colon", "token::", "REDACTED_value"},
		{"colon then equals", "a:=b", "REDACTED_value"},
		// A line the classifier cannot place becomes a whole-line marker.
		{"comment", "# owner: platform", "REDACTED_value"},
		{"slash comment", "  // note", "  REDACTED_value"},
		{"flagged comment", "# old token = abcdefghij", "REDACTED_api_token"},
		{"slack comment", "  # post with xoxb-0123456789ab here", "  REDACTED_slack_token"},
		{"bare list element", "  \"10.0.0.0/16\",", "  REDACTED_value"},
		{"bare number element", "  3,", "  REDACTED_value"},
		{"no value", "orphan =", "REDACTED_value"},
		{"unterminated string fails closed", "name = \"abc", "REDACTED_value"},
		{"unterminated flagged string", "password = \"abcdefghijk", "REDACTED_password_literal"},
		// A credential in the key itself never survives.
		{"aws key as map key", "  \"AKIAABCDEFGHIJKLMNOP\" = \"prod\"", "  REDACTED_aws_access_key"},
		{"stripe as map key", "  \"sk_live_abcdefgh12345678\" = 1", "  REDACTED_value"},
		{"key value inside a quoted key", "  \"client_secret = abcdefghij\": 1,", "  REDACTED_secret_literal"},
		// A quoted key is kept only when it reads as a name; free text in a key
		// (URL userinfo, a password with symbols, a CIDR, an ARN) takes the line.
		{"url userinfo as a key", "  \"https://u:hunter2@h.example.com\" = true", "  REDACTED_value"},
		{"symbols in a key", "  \"P@ss!w0rd\": 1,", "  REDACTED_value"},
		{"cidr key", "  \"10.0.0.0/16\" = \"office\"", "  REDACTED_value"},
		{"spaced key", "  \"my key\" = 1", "  REDACTED_value"},
		{"region key kept", "  \"us-east-1\" = 2", "  \"us-east-1\" = 2"},
		{"dotted key kept", "  \"app.example.com\": true,", "  \"app.example.com\": true,"},
		// Markers are a fixed point.
		{"existing value marker", "region = REDACTED_value", "region = REDACTED_value"},
		{"existing kind marker", "stripe_key = REDACTED_aws_access_key", "stripe_key = REDACTED_aws_access_key"},
		{"existing whole-line marker", "  REDACTED_slack_token", "  REDACTED_slack_token"},
	}
	for _, tc := range cases {
		if got := skeletonForTest(t, tc.in); got != tc.want {
			t.Errorf("%s:\n in   %q\n got  %q\n want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

const skeletonTfvars = `# Production
region         = "us-east-1"
instance_count = 3
db_password    = "hunter2hunter2"
app_secrets = {
  stripe = "sk_live_abcdefgh12345678"
  token  = "ghp0123456789abcdef"
}
user_data = <<-EOT
  #!/bin/bash
  export PASSWORD=supersecretvalue
  EOT
tls_key = <<EOF
-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEAabcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJ=
-----END RSA PRIVATE KEY-----
EOF
# -----BEGIN OPENSSH PRIVATE KEY-----
# b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQ
# -----END OPENSSH PRIVATE KEY-----
endpoint = "https://admin:hunter2@db.example.com/x"
enabled  = true
`

const skeletonTfvarsWant = `REDACTED_value
region         = REDACTED_value
instance_count = 3
db_password    = REDACTED_password_literal
app_secrets = {
  stripe = REDACTED_value
  token  = REDACTED_api_token
}
user_data = <<-EOT
  REDACTED_value
  REDACTED_password_literal
  EOT
tls_key = <<EOF
REDACTED_private_key
REDACTED_value
REDACTED_value
EOF
REDACTED_private_key
REDACTED_value
REDACTED_value
endpoint = REDACTED_value
enabled  = true
`

func TestRedactSkeletonTfvarsBody(t *testing.T) {
	t.Parallel()

	got, replaced, err := redactSkeleton(skeletonTfvars)
	if err != nil {
		t.Fatalf("redactSkeleton: %v", err)
	}
	if got != skeletonTfvarsWant {
		t.Fatalf("skeleton mismatch:\n got\n%s\n want\n%s", got, skeletonTfvarsWant)
	}
	if replaced != 14 {
		t.Errorf("replaced = %d, want 14", replaced)
	}
	for _, seed := range []string{"hunter2", "sk_live_", "ghp0123", "supersecret", "MIIEow", "b3BlbnNz", "us-east-1"} {
		if strings.Contains(got, seed) {
			t.Errorf("seed %q survived", seed)
		}
	}
}

const skeletonTfvarsJSON = `{
  "region": "us-east-1",
  "replicas": 2,
  "db_password": "hunter2hunter2",
  "nested": {
    "client_secret": "abcdefghijkl",
    "zones": [
      "a",
      "b"
    ]
  },
  "endpoint": "https://admin:hunter2@db.example.com/x"
}
`

const skeletonTfvarsJSONWant = `{
  "region": REDACTED_value,
  "replicas": 2,
  "db_password": REDACTED_value,
  "nested": {
    "client_secret": REDACTED_value,
    "zones": [
      REDACTED_value
      REDACTED_value
    ]
  },
  "endpoint": REDACTED_value
}
`

// TestRedactSkeletonTfvarsJSONBody covers the "key": value form. A value
// replacement keeps its trailing comma; a whole-line marker (a bare list
// element) does not, so every whole-line marker has one shape. Migration 138
// cannot see a JSON-quoted key (the closing quote sits between the key word and
// the colon), so db_password is no finding today and becomes REDACTED_value;
// #8833's detector migration adds JSON-quoted keys, and the redactor then marks
// the same lines with their kind through the shared detector.
func TestRedactSkeletonTfvarsJSONBody(t *testing.T) {
	t.Parallel()

	if got := skeletonForTest(t, skeletonTfvarsJSON); got != skeletonTfvarsJSONWant {
		t.Fatalf("skeleton mismatch:\n got\n%s\n want\n%s", got, skeletonTfvarsJSONWant)
	}
}

func TestRedactSkeletonPreservesLineGeometryAndCRLF(t *testing.T) {
	t.Parallel()

	in := "a = \"x\"\r\nb = 1\r\n\r\n# c\r\n"
	want := "a = REDACTED_value\r\nb = 1\r\n\r\nREDACTED_value\r\n"
	if got := skeletonForTest(t, in); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := skeletonForTest(t, "a = 1"); got != "a = 1" {
		t.Fatalf("no trailing newline: got %q", got)
	}
}

// TestRedactSkeletonFailsClosedAcrossLines covers state that crosses lines: an
// unclosed heredoc, an unclosed PEM block, and a block comment all redact to
// the end of the file or the block, never back to verbatim.
func TestRedactSkeletonFailsClosedAcrossLines(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, in, want string }{
		{"unclosed heredoc", "x = <<EOT\nenabled = true\nsecretish\n", "x = <<EOT\nREDACTED_value\nREDACTED_value\n"},
		{"unclosed pem", "# -----BEGIN PRIVATE KEY-----\nk = 1\nabc = true\n", "REDACTED_private_key\nREDACTED_value\nREDACTED_value\n"},
		// A quoted string that spans lines is invalid HCL (TokenQuotedNewline), so
		// the scan fails closed and every later line goes whole.
		{"pem in value", "k = \"-----BEGIN PRIVATE KEY-----\nAAAA = 1\n-----END PRIVATE KEY-----\"\nn = 2\n", "REDACTED_private_key\nREDACTED_value\nREDACTED_value\nREDACTED_value\n"},
		{"block comment", "/* header\n enabled = true\n*/\nn = 2\n", "REDACTED_value\n REDACTED_value\nREDACTED_value\nn = 2\n"},
		{"one-line block comment", "/* note */\nn = 2\n", "REDACTED_value\nn = 2\n"},
		{"slash star in a value does not open a comment", "p = \"/api/*\"\nn = 2\n", "p = REDACTED_value\nn = 2\n"},
		// A heredoc or block comment opened mid-expression is tracked, so its
		// body lines go whole (rev-8833 F3).
		{
			"heredoc in a list", "keys = [<<EOF\nPassw0rdHunter2: true\ndGhpc2lzYXNlY3JldHRva2Vu==\nEOF\n]\nn = 2\n",
			"keys = REDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\n]\nn = 2\n",
		},
		{
			"heredoc in a call", "policy = jsonencode(<<-EOT\n  Passw0rdHunter2: true\n  EOT\n)\n",
			"policy = REDACTED_value\n  REDACTED_value\n  REDACTED_value\nREDACTED_value\n",
		},
		{
			"heredoc as a bare element", "zones = [\n  <<EOF\n  Passw0rdHunter2: true\n  EOF\n]\n",
			"zones = [\n  REDACTED_value\n  REDACTED_value\n  REDACTED_value\n]\n",
		},
		{
			"block comment after a value", "x = 1 /* old\nPassw0rdHunter2: true\n*/\nn = 2\n",
			"x = REDACTED_value\nREDACTED_value\nREDACTED_value\nn = 2\n",
		},
		// The comment state is decided from the whole line, in both comment
		// branches, and before a heredoc tail (rev-8833 round 2, G1).
		{
			"second comment opens after a closed one at line start", "x = 1\n/* c */ y = 2 /* d\nPassw0rdHunter2: 1\n*/\nn = 2\n",
			"x = 1\nREDACTED_value\nREDACTED_value\nREDACTED_value\nn = 2\n",
		},
		{
			"comment opens after the closing line", "/* a\n*/ x = 1 /* b\nPassw0rdHunter2: 1\n*/\nn = 2\n",
			"REDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nn = 2\n",
		},
		{
			"heredoc marker inside an open comment", "x = 1 /* see <<EOF\nEOF\nPassw0rdHunter2: 1\n*/\nn = 2\n",
			"x = REDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nn = 2\n",
		},
		// A heredoc opened after a comment closes on the same line is a heredoc.
		{"heredoc after a one-line comment", "/* c */ x = <<EOF\nPassw0rdHunter2: 1\nEOF\nn = 2\n", "REDACTED_value\nREDACTED_value\nREDACTED_value\nn = 2\n"},
		{"heredoc after a comment closing line", "/* a\n*/ x = <<EOF\nPassw0rdHunter2: 1\nEOF\nn = 2\n", "REDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nn = 2\n"},
		{"comment closing line stays closed", "/* a\n*/ x = 1 /* b */\nn = 2\n", "REDACTED_value\nREDACTED_value\nn = 2\n"},
		{"block comment closed on its line", "x = 1 /* a */\nm = 2\n", "x = REDACTED_value\nm = 2\n"},
		{"slash star in a line comment opens nothing", "n = 1 # see /* here\nm = 2\n", "n = REDACTED_value\nm = 2\n"},
		{"bare token after a comment", "# rotated\ndGhpc2lzYXNlY3JldHRva2Vu==\n", "REDACTED_value\nREDACTED_value\n"},
		// A <<ID in a line comment or an unterminated string is text, not a
		// heredoc opener, so a block comment below it still opens (rev-8833
		// round 3, H1).
		{
			"heredoc marker in a # comment", "x = 1 # <<EOF\n/* c\nEOF\nPassw0rdHunter2: 1\n*/\nn = 2\n",
			"x = REDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nn = 2\n",
		},
		{
			"heredoc marker in a // comment", "x = 1 // <<EOF\n/* c\nEOF\nPassw0rdHunter2: 1\n*/\nn = 2\n",
			"x = REDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nn = 2\n",
		},
		{
			"dash heredoc marker in a # comment", "x = 1 # <<-EOF\n/* c\nEOF\nPassw0rdHunter2: 1\n*/\nn = 2\n",
			"x = REDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nn = 2\n",
		},
		{
			"squiggly marker is no heredoc", "x = 1 # <<~EOF\n/* c\nEOF\nPassw0rdHunter2: 1\n*/\nn = 2\n",
			"x = REDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nn = 2\n",
		},
		{
			"heredoc marker after a closed string and a comment", "x = \"a\" # <<EOF\n/* c\nEOF\nPassw0rdHunter2: 1\n*/\nn = 2\n",
			"x = REDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nn = 2\n",
		},
		{
			"heredoc marker in an unterminated string", "x = \"a # <<EOF\n/* c\nEOF\nPassw0rdHunter2: 1\n*/\nn = 2\n",
			"REDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\n",
		},
		{
			"block comment opener after the fake heredoc", "x = 1 # <<EOF\n/* c */ y = 2 /* d\nEOF\nPassw0rdHunter2: 1\n*/\nn = 2\n",
			"x = REDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nn = 2\n",
		},
		{
			"heredoc marker in a line-start comment", "# a <<EOF\n/* c\nEOF\nPassw0rdHunter2: 1\n*/\nn = 2\n",
			"REDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nn = 2\n",
		},
		// A <<ID after a closed string is code and does open a heredoc.
		{
			"heredoc after a string holding a hash", "x = \"#\" <<EOF\nPassw0rdHunter2: 1\nEOF\nn = 2\n",
			"x = REDACTED_value\nREDACTED_value\nREDACTED_value\nn = 2\n",
		},
		// Quotes inside ${ } nest: the # below sits in a string inside the
		// interpolation, so the real /* after the outer string still opens.
		{
			"comment after a template with a nested string", "x = \"${\"#\"}\" /* c\nPassw0rdHunter2: 1\n*/\nn = 2\n",
			"x = REDACTED_value\nREDACTED_value\nREDACTED_value\nn = 2\n",
		},
		{
			"heredoc after a template with a nested string", "x = \"${\"//\"}\" <<EOF\nPassw0rdHunter2: 1\nEOF\nn = 2\n",
			"x = REDACTED_value\nREDACTED_value\nREDACTED_value\nn = 2\n",
		},
		// An interpolation left open at the end of a line carries to the next
		// line: its lines are redacted whole, a heredoc opened in it redacts its
		// body, and a comment opened after it closes still opens.
		{
			"heredoc opened inside an interpolation", "x = \"${<<EOF\nPassw0rdHunter2: 1\nEOF\n}\"\nn = 2\n",
			"REDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nn = 2\n",
		},
		{
			"comment opened after a multi-line interpolation", "x = \"${\n}\" /* c\nPassw0rdHunter2: 1\n*/\nn = 2\n",
			"REDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nn = 2\n",
		},
		{
			"line inside a multi-line interpolation", "x = \"${\n  Passw0rdHunter2: 1\n}\"\nn = 2\n",
			"REDACTED_value\n  REDACTED_value\nREDACTED_value\nn = 2\n",
		},
		{
			"closed interpolation carries nothing", "x = \"${var.a}\"\nPassw0rdHunter2: 1\n",
			"x = REDACTED_value\nPassw0rdHunter2: 1\n",
		},
		// Round 4 (I1-I3): the lexical stack carries across lines and fails
		// closed. HCL: hclsyntax scan_tokens.rl.
		{
			"I1a heredoc in an interpolation", "x = \"${ <<EOF\nPassw0rdHunter2: 1\nEOF\n}\"\nn = 2\n",
			"REDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nn = 2\n",
		},
		{
			"I1b comment after a multi-line interpolation", "x = \"${ a\n}\" /* c\nPassw0rdHunter2: 1\n*/\nn = 2\n",
			"REDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nn = 2\n",
		},
		{
			"I3 heredoc nested in a heredoc interpolation", "x = <<EOF\n${ <<INNER\nEOF\nPassw0rdHunter2: 1\nINNER\n}\nEOF\nn = 2\n",
			"x = <<EOF\nREDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nEOF\nn = 2\n",
		},
		{
			"marker after an interpolation is text", "x = <<EOF\n${a}EOF\nPassw0rdHunter2: 1\nEOF\nn = 2\n",
			"x = <<EOF\nREDACTED_value\nREDACTED_value\nEOF\nn = 2\n",
		},
		{
			"escaped openers in a heredoc are text", "x = <<EOF\n$${ a\n%%{ b\nEOF\nPassw0rdHunter2: 1\n",
			"x = <<EOF\nREDACTED_value\nREDACTED_value\nEOF\nPassw0rdHunter2: 1\n",
		},
		{
			"line comment in an interpolation keeps the stack", "x = \"${ # c\n}\" /* d\nPassw0rdHunter2: 1\n*/\nn = 2\n",
			"REDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nn = 2\n",
		},
		{
			"directive opens an interpolation", "x = \"%{ if a\n}\" /* c\nPassw0rdHunter2: 1\n*/\nn = 2\n",
			"REDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nn = 2\n",
		},
		{
			"CRLF heredoc", "x = <<EOF\r\nPassw0rdHunter2: 1\r\nEOF\r\nn = 2\r\n",
			"x = <<EOF\r\nREDACTED_value\r\nEOF\r\nn = 2\r\n",
		},
		// An unterminated quoted literal is sticky: nothing later clears it.
		{
			"unterminated literal fails the rest closed", "x = \"abc\nPassw0rdHunter2: 1\ny = \"z\"\nw = 1\n",
			"REDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\n",
		},
		{
			"a comment close does not clear a failure", "x = \"abc\n*/\nPassw0rdHunter2: 1\nn = 2\n",
			"REDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\n",
		},
		{
			"a heredoc close does not clear a failure", "x = \"abc\ny = <<EOF\nEOF\nPassw0rdHunter2: 1\n",
			"REDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\n",
		},
		{
			"escaped template opener is plain text", "x = \"$${\" /* c\nPassw0rdHunter2: 1\n*/\nn = 2\n",
			"x = REDACTED_value\nREDACTED_value\nREDACTED_value\nn = 2\n",
		},
	}
	for _, tc := range cases {
		if got := skeletonForTest(t, tc.in); got != tc.want {
			t.Errorf("%s:\n got  %q\n want %q", tc.name, got, tc.want)
		}
	}
}

func TestRedactSkeletonRejectsNonText(t *testing.T) {
	t.Parallel()

	for name, in := range map[string]string{
		"invalid utf-8": "a = \"\xff\"\n",
		"nul":           "a = 1\x00\n",
		"too large":     strings.Repeat("a", MaxGovernedBodyBytes+1),
	} {
		if out, _, err := redactSkeleton(in); err == nil {
			t.Errorf("%s: accepted, returned %d bytes", name, len(out))
		}
	}
}

// TestRedactSkeletonNestingDepth checks the depth bound: 16 nested "${ levels
// (32 frames) lex, one more passes maxScanDepth and fails the rest of the body
// closed (rev-8833 round 4, I2).
func TestRedactSkeletonNestingDepth(t *testing.T) {
	t.Parallel()

	nested := func(levels int) string {
		return "x = " + strings.Repeat(`"${`, levels) + "a" + strings.Repeat(`}"`, levels) +
			"\n/* c\nPassw0rdHunter2: 1\n*/\nn = 2\n"
	}
	for _, tc := range []struct {
		name   string
		levels int
		want   string
	}{
		{"16 levels", 16, "x = REDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nn = 2\n"},
		{"17 levels", 17, "x = REDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\nREDACTED_value\n"},
	} {
		if got := skeletonForTest(t, nested(tc.levels)); got != tc.want {
			t.Errorf("%s:\n got  %q\n want %q", tc.name, got, tc.want)
		}
	}
}
