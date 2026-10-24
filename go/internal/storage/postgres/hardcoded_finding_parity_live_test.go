// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8833 engine-parity proof that migration 234's eshu_secret_line_findings (Postgres ARE) and go/internal/secret/detect.Finding (Go RE2) report the same (line_number, finding_kind) for every line of the gap-table, six-kind marker, extras, and multi-line corpus, with the known Go-superset Unicode lines asserted explicitly; isolated bootstrapped schema per test
//eshu:readiness:env ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/secret/detect"
)

// secretParityCorpus is the engine-parity corpus: the #8813 gap table, the
// six-kind marker probe (raw and marked, key and free-text lines), the
// extras, and multi-line bodies whose file-level prefilter differs from any
// one line (a key and value split across lines, a mid-file block opener).
var secretParityCorpus = []string{
	// Gap table (8813 evidence page).
	"npmAuthToken: abcdef123456", `npmAuthToken: "${NPM_TOKEN}"`, `npmAuthIdent: "dXNlcjpwYXNzd29yZA=="`,
	`npmRegistryServer: "https://user:pass@npm.example.com/"`, `httpsProxy: "http://u:p@proxy.example.com:3128"`,
	`db_password = "hunter2hunter2"`, `"db_password": "hunter2hunter2"`, `access_key = "abcdefghij"`,
	`connection_string = "Server=x;Password=abcdefgh"`, "app_secrets = {", `  stripe = "sk_live_abcdefgh12345678"`,
	"db_password = var.db_password", `api_token = "abc"`,
	// Six-kind marker probe, raw then marked.
	`access_key_id = "AKIAABCDEFGHIJKLMNOP"`, `access_key_id = "REDACTED_aws_access_key"`,
	"# rotate AKIAABCDEFGHIJKLMNOP soon", "# rotate REDACTED_aws_access_key soon",
	`slack_webhook = "xoxb-0123456789ab"`, `slack_webhook = "REDACTED_slack_token"`,
	"# post with xoxb-0123456789ab here", "# post with REDACTED_slack_token here",
	`key = "-----BEGIN RSA PRIVATE KEY-----"`, `key = "REDACTED_private_key"`,
	"# -----BEGIN RSA PRIVATE KEY-----", "# REDACTED_private_key",
	"npmAuthToken: REDACTED_api_token", "# the api_key=REDACTED_api_token was pasted",
	`db_password = "REDACTED_password_literal"`, "# password: REDACTED_password_literal per ops",
	`client_secret = "REDACTED_secret_literal"`, "# the client_secret = REDACTED_secret_literal shared",
	"npmAuthToken: REDACTED_aws_access_key", `slack_token = "REDACTED_slack_token"`,
	"REDACTED_aws_access_key", "  REDACTED_value", `db_password = "REDACTED_value"`, "x = REDACTED_valuable",
	"redacted_api_token", "token: redacted_api_token",
	// Extras and their near misses.
	`npm_auth_ident = "dXNlcjpwYXNzd29yZA=="`, `'client_secret': 'abcdefghij'`, `"api_key" : "abcdefghij"`,
	`"//user:pass@npm.example.com":`, `url = "https://user@example.com/"`, "// see user:pass@host",
	"  db_passwords = {  ", "service_tokens = {", "credentials = {", `"nextPageToken": {`,
	`app_secrets = { stripe = "x" }`, "secrets: abcdefghij", "app_secrets = {\r",
	"nothing to see", "", "password\u2028: abcdefghij", "password\u3000: abcdefghij", "api_key:\u2003abcdefghij",
	// Multi-line bodies.
	"password:\n  abcdefghij",
	"region = 1\ncredentials = {\n  a = 1\n}\napp_secrets = {\r\nx = 2",
	"a = 1\nkey = \"-----BEGIN PRIVATE KEY-----\nMIIEabc\n-----END PRIVATE KEY-----\"\nb = REDACTED_value",
	"npmScopes:\n  acme:\n    npmAuthToken: REDACTED_api_token\n    npmRegistryServer: \"https://REDACTED_password_literal@acme.example.com\"",
}

// secretParityGoSuperset lists the lines where the engines differ on purpose:
// Go's (?i) folds U+017F and U+212A, and detect.SpaceClass adds Unicode spaces
// Postgres [[:space:]] lacks (U+00A0; U+2028 and U+3000 match in both, measured
// on PostgreSQL 18.6). Go finds these; Postgres does not. A superset is safe
// for redaction (it redacts more).
var secretParityGoSuperset = []string{
	"\u017fecret: abcdefgh12345", "api_\u212aey=abcdefgh12345", "password\u00a0: abcdefghij",
}

func TestSecretDetectorEngineParity(t *testing.T) {
	proof := openReprojectProof(t)
	findings := func(body string) []string {
		rows, err := proof.db.QueryContext(t.Context(),
			"SELECT line_number, finding_kind FROM eshu_secret_line_findings($1) ORDER BY line_number", body)
		if err != nil {
			t.Fatalf("eshu_secret_line_findings: %v", err)
		}
		defer func() { _ = rows.Close() }()
		var out []string
		for rows.Next() {
			var line int
			var kind string
			if err := rows.Scan(&line, &kind); err != nil {
				t.Fatalf("scan: %v", err)
			}
			out = append(out, fmt.Sprintf("%d:%s", line, kind))
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("rows: %v", err)
		}
		return out
	}
	goFindings := func(body string) []string {
		var out []string
		for i, line := range strings.Split(body, "\n") {
			if kind := detect.Finding(line); kind != "" {
				out = append(out, fmt.Sprintf("%d:%s", i+1, kind))
			}
		}
		return out
	}
	kinds := map[string]bool{}
	for _, body := range secretParityCorpus {
		got, want := findings(body), goFindings(body)
		if !slices.Equal(got, want) {
			t.Errorf("engines disagree on %q: postgres %v, go %v", body, got, want)
		}
		for _, finding := range got {
			kinds[finding[strings.IndexByte(finding, ':')+1:]] = true
		}
	}
	for _, kind := range detect.Kinds() {
		if !kinds[kind] {
			t.Errorf("the corpus exercises no %s finding", kind)
		}
	}
	for _, line := range secretParityGoSuperset {
		if got, want := findings(line), goFindings(line); len(got) != 0 || len(want) != 1 {
			t.Errorf("superset line %q: postgres %v (want none), go %v (want one finding)", line, got, want)
		}
	}
}
