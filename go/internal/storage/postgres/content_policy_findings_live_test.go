// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8833 disposable PostgreSQL proof that a seeded .yarnrc.yml and terraform.tfvars, stored through the default registry's marker and skeleton policies and the production content writer, keep every raw hardcoded-secret finding at its original line with its raw kind in content_file_secret_lines (the side table investigate_hardcoded_secrets reads) under migration 233, and that no seeded credential reaches content, content_lower, or line_text; isolated bootstrapped schema per test
//eshu:readiness:env ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	gitcontent "github.com/eshu-hq/eshu/go/internal/collector/git/content"
	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/secret/detect"
)

const policyFindingsYarnrc = `nodeLinker: node-modules
npmAuthToken: YarnTokSeed0123456789
npmScopes:
  acme:
    npmAuthToken: AKIAABCDEFGHIJKLMNOP
    npmRegistryServer: "https://ci:YarnPassSeed99@npm.example.com/"
# old token xoxb-0123456789abYarnSlackSeed
npmAuthIdent: "YarnIdentSeed0123456789"
unsafeHttpWhitelist:
  - "*.example.com"
npmPublishRegistry: "${NPM_PUBLISH}"
shortToken:
  npmAuthToken: abcde
`

const policyFindingsTfvars = `db_password = "TfPwSeed0123456789"
app_secrets = {
  stripe = "TfStripeSeed0123456789"
  token  = "TfTokSeed0123456789"
}
user_data = <<EOT
export PASSWORD=TfHeredocSeed0123
EOT
# legacy api_key = TfCommentSeed0123
endpoint = "https://svc:TfUrlSeed0123@db.example.com/x"
instance_count = 3
`

var policyFindingsSeeds = []string{
	"YarnTokSeed", "AKIAABCDEFGHIJKLMNOP", "YarnPassSeed", "YarnSlackSeed", "YarnIdentSeed",
	"TfPwSeed", "TfStripeSeed", "TfTokSeed", "TfHeredocSeed", "TfCommentSeed", "TfUrlSeed",
}

// TestContentPolicyKeepsEveryRawFindingLive proves the #8833 acceptance on the
// production write path: the stored bodies keep every raw finding at its line
// with its raw kind (the skeleton exactly; marker mode adds only the accepted
// gains from a short value under a listed key), and no seed is stored.
func TestContentPolicyKeepsEveryRawFindingLive(t *testing.T) {
	proof := openReprojectProof(t)
	ctx, db := t.Context(), proof.db
	registry := parser.DefaultRegistry()
	files := map[string]string{"web/.yarnrc.yml": policyFindingsYarnrc, "infra/terraform.tfvars": policyFindingsTfvars}
	records := make([]content.Record, 0, len(files))
	for path, raw := range files {
		stored := gitcontent.ApplyContentStore(registry.ContentStoreForPath(path), raw)
		if stored.Outcome != gitcontent.StoreOutcomeRedacted {
			t.Fatalf("%s: outcome %q, want redacted", path, stored.Outcome)
		}
		records = append(records, content.Record{Path: path, Body: stored.Body})
	}
	writer := NewContentWriter(SQLDB{DB: db})
	if _, err := writer.Write(ctx, content.Materialization{RepoID: "repo-8833", ScopeID: "scope-8833", GenerationID: "gen-8833", Records: records}); err != nil {
		t.Fatalf("write content: %v", err)
	}

	stored := map[string][]string{}
	rows, err := db.QueryContext(ctx, `SELECT relative_path, line_number, finding_kind, line_text
		FROM content_file_secret_lines WHERE repo_id = 'repo-8833' ORDER BY relative_path, line_number`)
	if err != nil {
		t.Fatalf("read side table: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var path, kind, text string
		var line int
		if err := rows.Scan(&path, &line, &kind, &text); err != nil {
			t.Fatalf("scan: %v", err)
		}
		stored[path] = append(stored[path], fmt.Sprintf("%d:%s", line, kind))
		for _, seed := range policyFindingsSeeds {
			if strings.Contains(text, seed) {
				t.Errorf("%s:%d line_text holds seed %q", path, line, seed)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	for path, raw := range files {
		var want []string
		for i, line := range strings.Split(raw, "\n") {
			if kind := detect.Finding(line); kind != "" {
				want = append(want, fmt.Sprintf("%d:%s", i+1, kind))
			}
		}
		got := stored[path]
		switch path {
		case "infra/terraform.tfvars":
			if !slices.Equal(got, want) {
				t.Errorf("%s findings = %v, want the raw findings %v", path, got, want)
			}
		case "web/.yarnrc.yml":
			for _, finding := range want {
				if !slices.Contains(got, finding) {
					t.Errorf("%s lost raw finding %s (got %v)", path, finding, got)
				}
			}
			// The accepted gain: a 5-character token under a listed key is
			// redacted and its marker is a finding.
			if gained := len(got) - len(want); gained != 1 || !slices.Contains(got, "13:api_token") {
				t.Errorf("%s findings = %v over raw %v, want exactly the 5-character token gain at line 13", path, got, want)
			}
		}
	}
	for _, check := range []string{"2:api_token", "5:aws_access_key", "6:password_literal", "7:slack_token", "8:secret_literal"} {
		if !slices.Contains(stored["web/.yarnrc.yml"], check) {
			t.Errorf(".yarnrc.yml lacks finding %s: %v", check, stored["web/.yarnrc.yml"])
		}
	}
	if !slices.Contains(stored["infra/terraform.tfvars"], "1:password_literal") {
		t.Errorf("terraform.tfvars lacks the password finding at line 1: %v", stored["infra/terraform.tfvars"])
	}

	var leaks int
	pattern := strings.Join(policyFindingsSeeds, "|")
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM content_files
		WHERE repo_id = 'repo-8833' AND (content ~ $1 OR content_lower ~ lower($1))`, pattern).Scan(&leaks); err != nil {
		t.Fatalf("leak check: %v", err)
	}
	if leaks != 0 {
		t.Errorf("%d content_files rows hold a seeded credential in content or content_lower", leaks)
	}
}
