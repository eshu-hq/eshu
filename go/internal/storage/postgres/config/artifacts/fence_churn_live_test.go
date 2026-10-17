// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8155 S3 arm 6 as a PR test through the production ContentWriter and GenerationRetentionStore: exact version rewrites full 10, delta 2, reap 4, bulk 1, prune 1 per repository, with a seeded per-row unguarded fence that misses the counts (RED); enrolled in the live-postgres-readiness runner; 11-17 s per proof on a local PostgreSQL 18.6 on 2026-10-16; wall time is logged, not gated
//eshu:readiness:env ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
)

// wantFenceChurn is S3 arm 6 (re-ruling 2): one version-row rewrite per
// repository per content transaction, never per row or per Write. A Write
// sends tombstones and reaped paths through one delete list chunked at 500,
// plus one transaction per 500-row upsert batch.
var wantFenceChurn = map[string]int64{
	"full":  10, // 5,000 files: 10 upsert batches
	"delta": 2,  // 1 changed file + 3 tombstones: 1 delete chunk, 1 upsert batch
	"reap":  4,  // 497 files + 3 tombstones + 1,200 reaped paths: 3 delete chunks, 1 upsert batch
	"bulk":  1,  // one 50,000-row INSERT statement
	"prune": 1,  // per affected repository, one retention transaction
}

// measureFenceChurn runs each arm 6 workload through the production writers
// and returns the version delta of each workload's repository. The prune
// workload reports the delta its three repositories share.
func measureFenceChurn(ctx context.Context, t *testing.T, db *sql.DB) map[string]int64 {
	t.Helper()
	got := map[string]int64{}
	delta := func(repo string, run func()) int64 {
		before := versionOf(ctx, t, db, repo)
		run()
		return versionOf(ctx, t, db, repo) - before
	}
	seedZeroVersions(ctx, t, db, "repo-full", "repo-delta", "repo-reap", "repo-bulk")

	got["full"] = delta("repo-full", func() {
		writeContent(ctx, t, db, fileMaterialization("repo-full", "scope-full", "src", 5000))
	})

	writeContent(ctx, t, db, fileMaterialization("repo-delta", "scope-delta", "src", 10))
	got["delta"] = delta("repo-delta", func() {
		mat := content.Materialization{RepoID: "repo-delta", ScopeID: "scope-delta", GenerationID: "gen-delta-2"}
		mat.Records = append(mat.Records, content.Record{Path: "src/m00000.tf", Body: "changed"})
		for _, p := range []string{"src/m00001.tf", "src/m00002.tf", "src/m00003.tf"} {
			mat.Records = append(mat.Records, content.Record{Path: p, Deleted: true})
		}
		writeContent(ctx, t, db, mat)
	})

	writeContent(ctx, t, db, fileMaterialization("repo-reap", "scope-reap", "src", 1700))
	got["reap"] = delta("repo-reap", func() {
		mat := fileMaterialization("repo-reap", "scope-reap", "src", 497)
		mat.GenerationID = "gen-reap-2"
		mat.FullSnapshot = true
		for f := 497; f < 500; f++ {
			mat.Records = append(mat.Records, content.Record{Path: fmt.Sprintf("src/m%05d.tf", f), Deleted: true})
		}
		writeContent(ctx, t, db, mat)
	})
	if n := contentCount(ctx, t, db, "repo-reap", "src"); n != 497 {
		t.Fatalf("reap left %d rows, want 497", n)
	}

	got["bulk"] = delta("repo-bulk", func() { insertContentRows(ctx, t, db, "bulk", 50000, "repo-bulk") })

	pruneRepos := []string{"repo-prune-1", "repo-prune-2", "repo-prune-3"}
	seedZeroVersions(ctx, t, db, pruneRepos...)
	var scopes []retentionScope
	for i, repo := range pruneRepos {
		scopes = append(scopes, retentionScope{fmt.Sprintf("scope-prune-%d", i), repo, "old", 700})
	}
	seedRetentionScopes(ctx, t, db, scopes)
	before := versionsOf(ctx, t, db, pruneRepos)
	result, err := prune(ctx, db)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if result.RowsPruned["content_files"] != 2100 {
		t.Fatalf("prune removed %d content rows, want 2100", result.RowsPruned["content_files"])
	}
	got["prune"] = -1
	for i, repo := range pruneRepos {
		d := versionOf(ctx, t, db, repo) - before[repo]
		if i > 0 && d != got["prune"] {
			t.Errorf("prune bumped repositories unevenly: %s by %d, an earlier one by %d", repo, d, got["prune"])
		}
		got["prune"] = d
	}
	return got
}

// TestVersionFenceChurnIsOnePerTransactionLive is S3 arm 6 as a PR test:
// exact version deltas for a full Write, a delta Write, a full-snapshot reap,
// a bulk insert and a retention prune.
func TestVersionFenceChurnIsOnePerTransactionLive(t *testing.T) {
	ctx, db := openDatabase(t)
	got := measureFenceChurn(ctx, t, db)
	for workload, want := range wantFenceChurn {
		if got[workload] != want {
			t.Errorf("%s churn = %d version rewrites, want %d", workload, got[workload], want)
		}
	}
}

// seededPerRowFence is the seeded RED: a row-level fence with no
// once-per-transaction guard, the shape arm 6 exists to reject.
const seededPerRowFence = `
CREATE FUNCTION zz_seeded_per_row_bump() RETURNS trigger LANGUAGE plpgsql AS $fn$
BEGIN
    INSERT INTO config_artifact_content_versions AS v (repo_id, version, bumped_xid, bumped_at)
    VALUES (coalesce(NEW.repo_id, OLD.repo_id), 1, pg_current_xact_id(), now())
    ON CONFLICT (repo_id) DO UPDATE SET version = v.version + 1;
    RETURN NULL;
END
$fn$;
CREATE TRIGGER content_files_version_fence_insert AFTER INSERT OR UPDATE OR DELETE ON content_files
    FOR EACH ROW EXECUTE FUNCTION zz_seeded_per_row_bump();`

// TestVersionFenceChurnRejectsSeededPerRowFenceLive proves the churn test is
// sensitive: with the seeded per-row fence the same measurement misses the
// arm 6 counts.
func TestVersionFenceChurnRejectsSeededPerRowFenceLive(t *testing.T) {
	ctx, db := openDatabase(t)
	dropFence(ctx, t, db)
	mustExec(ctx, t, db, seededPerRowFence)
	got := measureFenceChurn(ctx, t, db)
	if got["full"] == wantFenceChurn["full"] || got["bulk"] == wantFenceChurn["bulk"] {
		t.Fatalf("seeded per-row fence matched arm 6 counts (full %d, bulk %d): the churn test cannot fail",
			got["full"], got["bulk"])
	}
	t.Logf("seeded per-row fence churn (RED as intended): %v", got)
}
