// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/support"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const (
	storySupportIssueIndexName = "fact_records_story_support_issue_idx"

	// jiraIssueLinkBufferBound is the shared-buffer ceiling for the Jira issue-link
	// read at the #7138 corpus plus 20,000 issues. The keyed probe measures about
	// 7,000 hits; without migration 155 the second hop measured 344,516 hits and
	// 952 ms, so the bound catches that regression without being a timing check.
	jiraIssueLinkBufferBound = 25_000
)

// TestServiceStoryJiraIssueLinkUsesIssueIndexLive is the #7464 plan proof, on the
// corpus the #7138 proof used (about 600,000 fact_records, three generations per
// scope, only the last active) with its Jira scope given 20,000 issues: every
// external link carries the id of one of them, and each issue has a record and
// one or two transitions. repo-x is linked by 500 of the links.
//
// The issue-link read must start from the repository's links (migration 152),
// reach each linked issue's records through migration 155's index with the issue
// id as an index condition, and skip the transition probe once the records fill
// the bound, in custom and generic plans. The source-only count keeps migration
// 123's index with the linked-issue set folded in.
//
// Set ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN (an administrative
// postgres-database DSN) and ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE=1.
func TestServiceStoryJiraIssueLinkUsesIssueIndexLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE"),
		30*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}
	seedStorySupportScaleCorpus(t, ctx, db)
	seedStorySupportJiraIssueCorpus(t, ctx, db)

	issueSQL, issueArgs := support.JiraIssueLinkSQL("repo-x", 10)
	for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
		plan := explainPreparedWithMode(t, ctx, db, issueSQL, issueArgs, mode)
		for _, index := range []string{storySupportLinkRepoIndexName, storySupportIssueIndexName} {
			if !plan.indexes[index] {
				t.Fatalf("issue-link/%s: plan did not use %s: indexes=%v", mode, index, plan.indexNames())
			}
		}
		if !planIndexCondMentions(plan, storySupportIssueIndexName, "provider_work_item_id") {
			t.Fatalf("issue-link/%s: the issue probe is a heap filter, not an index condition: conds=%v", mode, plan.indexConds[storySupportIssueIndexName])
		}
		if plan.sharedHit+plan.sharedRead > jiraIssueLinkBufferBound {
			t.Fatalf("issue-link/%s: %d shared buffers, want under %d; the second hop has left migration 155's index", mode, plan.sharedHit+plan.sharedRead, jiraIssueLinkBufferBound)
		}
		t.Logf("STORY_SUPPORT_PLAN jira-issue-link repo-x %s ms=%.2f buffers=hit:%d/read:%d", mode, plan.executionMS, plan.sharedHit, plan.sharedRead)
	}

	rows, err := db.QueryContext(ctx, issueSQL, issueArgs...)
	if err != nil {
		t.Fatalf("issue-link read: %v", err)
	}
	defer func() { _ = rows.Close() }()
	count := 0
	for rows.Next() {
		count++
	}
	if count != 11 {
		t.Fatalf("issue-link read returned %d rows, want the bound limit+1 = 11", count)
	}

	sourceSQL, sourceArgs := buildServiceStoryTargetSupportSourceOnlySQL(serviceStoryTargetSupportFactKinds())
	for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
		plan := explainPreparedWithMode(t, ctx, db, sourceSQL, sourceArgs, mode)
		if !plan.indexes[storySupportKindsIndexName] {
			t.Fatalf("source-only/%s: plan did not use %s: indexes=%v", mode, storySupportKindsIndexName, plan.indexNames())
		}
		if plan.sharedHit+plan.sharedRead > sourceOnlyBufferBound {
			t.Fatalf("source-only/%s: %d shared buffers, want under %d", mode, plan.sharedHit+plan.sharedRead, sourceOnlyBufferBound)
		}
		t.Logf("STORY_SUPPORT_PLAN source-only+issues %s ms=%.2f buffers=hit:%d/read:%d", mode, plan.executionMS, plan.sharedHit, plan.sharedRead)

		// The same statement without the Jira issue term, on the same corpus, is the
		// "before" figure: what the extra correctness costs.
		before := strings.Replace(
			strings.Replace(sourceSQL, support.LinkedIssuesSQL(), "", 1),
			"OR "+support.LinkedIssuePredicate()+"\n  ", "", 1)
		if before == sourceSQL {
			t.Fatal("could not derive the statement without the Jira issue term")
		}
		without := explainPreparedWithMode(t, ctx, db, before, sourceArgs, mode)
		t.Logf("STORY_SUPPORT_PLAN source-only-before %s ms=%.2f buffers=hit:%d/read:%d", mode, without.executionMS, without.sharedHit, without.sharedRead)
	}
}

// seedStorySupportJiraIssueCorpus gives the #7138 Jira scope its issues: every
// external link of every generation carries a provider_work_item_id, and the
// active generation gains 20,000 records and 30,000 transitions keyed to the same
// ids. The superseded generations get a tenth of them, so the index also holds
// facts the read must exclude.
func seedStorySupportJiraIssueCorpus(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	execProofStatements(t, ctx, db, []proofStatement{{`
UPDATE fact_records
SET payload = payload || jsonb_build_object('provider_work_item_id', 'iss-' || (1 + (substring(fact_id from '[0-9]+$')::int % 20000)))
WHERE scope_id = 'scope:jira:1' AND fact_kind = 'work_item.external_link'`, nil}, {`
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, collector_kind,
                          source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'jr:' || g || ':' || n, 'scope:jira:1', 'gen:jira:1:' || g, 'work_item.record',
       'jr:' || g || ':' || n, 'jira', 'jira', 'jr:' || g || ':' || n, clock_timestamp(), clock_timestamp(),
       jsonb_build_object('provider_work_item_id', 'iss-' || n, 'work_item_key', 'K-' || n)
FROM generate_series(1, 3) AS g, generate_series(1, 20000) AS n
WHERE g = 3 OR n % 10 = 0`, nil}, {`
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, collector_kind,
                          source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'jt:' || g || ':' || n, 'scope:jira:1', 'gen:jira:1:' || g, 'work_item.transition',
       'jt:' || g || ':' || n, 'jira', 'jira', 'jt:' || g || ':' || n, clock_timestamp(), clock_timestamp(),
       jsonb_build_object('provider_work_item_id', 'iss-' || (1 + n % 20000), 'work_item_key', 'K-' || (1 + n % 20000))
FROM generate_series(1, 3) AS g, generate_series(1, 30000) AS n
WHERE g = 3 OR n % 10 = 0`, nil}, {`ANALYZE fact_records`, nil}})
}
