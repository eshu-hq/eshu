// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8155 disposable PostgreSQL 18 proof of the walker walk filter: only the repository over the failure limit for the current extractor version is reported skipped; under-limit, old-extractor-failure, and successful repositories are not; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/config/artifacts"
)

// TestSkippedReposPredicateLive seeds one state row per skip case and checks
// the walk filter (protocol section 4, skip rule): over the limit for the
// current extractor is skipped; below the limit, failed for an older
// extractor, and successfully derived repositories are work, not skips.
func TestSkippedReposPredicateLive(t *testing.T) {
	ctx, db := openDatabase(t)
	const extractor = 1
	seedVersions(ctx, t, db, map[string]int64{
		"r-skip": 1, "r-under-limit": 1, "r-old-extractor": 1, "r-ok": 1,
	})
	for _, state := range []struct {
		repo                 string
		extractorVersion     int
		failures             int
		lastFailureExtractor int
	}{
		{"r-skip", extractor, 5, extractor},
		{"r-under-limit", extractor, 2, extractor},
		{"r-old-extractor", extractor, 5, extractor - 1},
	} {
		if _, err := db.ExecContext(ctx, `
INSERT INTO config_artifact_repo_state
    (repo_id, extractor_version, failure_count, last_error_class,
     last_failure_at, last_failure_extractor_version)
VALUES ($1, $2, $3, 'error', now(), $4)`,
			state.repo, state.extractorVersion, state.failures, state.lastFailureExtractor); err != nil {
			t.Fatalf("seed failure state for %s: %v", state.repo, err)
		}
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO config_artifact_repo_state
    (repo_id, content_version, extractor_version, row_count, files_listed,
     candidate_count, candidates_read, list_truncated, derived_at, failure_count)
VALUES ('r-ok', 1, $1, 0, 0, 0, 0, false, now(), 0)`, extractor); err != nil {
		t.Fatalf("seed success state: %v", err)
	}

	got, err := artifacts.SkippedRepos(ctx, postgres.SQLDB{DB: db}, artifacts.SkippedRequest{
		ExtractorVersion:  extractor,
		SkipAfterFailures: 5,
	}, []string{"r-skip", "r-under-limit", "r-old-extractor", "r-ok", "r-unknown"})
	if err != nil {
		t.Fatalf("SkippedRepos() error = %v", err)
	}
	if !reflect.DeepEqual(got, []string{"r-skip"}) {
		t.Fatalf("SkippedRepos() = %v, want [r-skip]", got)
	}
}
