// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8833 disposable PostgreSQL proof that eshu_dp_content_policy_raw_rows (ContentPolicyRawRowsStore) counts the verbatim credential lines of .yarnrc.yml and tfvars rows written before the content-store binding, and reads zero after the next generation stores the same files through the default registry's marker and skeleton policies; isolated bootstrapped schema per test
//eshu:readiness:env ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN

import (
	"testing"

	gitcontent "github.com/eshu-hq/eshu/go/internal/collector/git/content"
	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

// TestContentPolicyRawRowsHealsAfterTheNextGeneration is the #8833 exposure
// gauge acceptance: it seeds verbatim rows first (the state a deployment holds
// before the binding), checks the gauge counts them, then writes the next
// generation through the policies and checks the gauge reads zero. An
// unrelated file's finding never counts.
func TestContentPolicyRawRowsHealsAfterTheNextGeneration(t *testing.T) {
	proof := openReprojectProof(t)
	ctx, db := t.Context(), proof.db
	registry := parser.DefaultRegistry()
	store := NewContentPolicyRawRowsStore(SQLDB{DB: db}, registry.ContentStorePathPattern())
	writer := NewContentWriter(SQLDB{DB: db})
	files := map[string]string{
		"web/.yarnrc.yml":        policyFindingsYarnrc,
		"infra/terraform.tfvars": policyFindingsTfvars,
	}
	write := func(generation string, transform func(path, body string) string) {
		t.Helper()
		records := []content.Record{{Path: "src/app.go", Body: "package app\nconst password = \"AppPwSeed0123456789\"\n"}}
		for path, body := range files {
			records = append(records, content.Record{Path: path, Body: transform(path, body)})
		}
		if _, err := writer.Write(ctx, content.Materialization{RepoID: "repo-raw", ScopeID: "scope-raw", GenerationID: generation, Records: records}); err != nil {
			t.Fatalf("write %s: %v", generation, err)
		}
	}

	write("gen-verbatim", func(_, body string) string { return body })
	seeded, err := store.ContentPolicyRawRows(ctx)
	if err != nil {
		t.Fatalf("count seeded: %v", err)
	}
	// .yarnrc.yml: token, AWS key, Slack comment, npmAuthIdent (4); tfvars:
	// password, token, heredoc password, comment api_key (4). Both userinfo
	// lines name example.com, so the suppressed column hides them, and the
	// gauge counts unsuppressed rows only. app_secrets = { is a finding with no
	// value and is not counted.
	if seeded != 8 {
		t.Fatalf("seeded raw rows = %d, want 8 (the test would pass on an empty database)", seeded)
	}

	write("gen-policy", func(path, body string) string {
		return gitcontent.ApplyContentStore(registry.ContentStoreForPath(path), body).Body
	})
	healed, err := store.ContentPolicyRawRows(ctx)
	if err != nil {
		t.Fatalf("count healed: %v", err)
	}
	if healed != 0 {
		t.Fatalf("raw rows after the policy generation = %d, want 0", healed)
	}
	var unrelated int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM content_file_secret_lines
		WHERE repo_id = 'repo-raw' AND relative_path = 'src/app.go'`).Scan(&unrelated); err != nil {
		t.Fatalf("count unrelated: %v", err)
	}
	if unrelated != 1 {
		t.Fatalf("unrelated finding rows = %d, want 1 (the gauge must ignore it, not the side table)", unrelated)
	}
}

// TestContentPolicyRawRowsIsZeroWithoutAPolicy checks the store reports zero
// and runs no query shape error when no definition is governed.
func TestContentPolicyRawRowsIsZeroWithoutAPolicy(t *testing.T) {
	proof := openReprojectProof(t)
	count, err := NewContentPolicyRawRowsStore(SQLDB{DB: proof.db}, "").ContentPolicyRawRows(t.Context())
	if err != nil || count != 0 {
		t.Fatalf("count = %d, err = %v, want 0 and no error", count, err)
	}
}
