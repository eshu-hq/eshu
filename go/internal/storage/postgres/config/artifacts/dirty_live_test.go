// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/config/artifacts"
)

// TestListDirtyFindsTheWalkersWorkLive seeds one repository per dirty-join
// case and checks the walker's work list (protocol sections 1 and 4): failed
// derives and old extractor versions first, then repositories with no state
// row, then moved content; a repository over the failure limit for the
// current extractor is listed last and marked skipped; exact rows, rows from
// a newer extractor, and a state row with no version row are not work.
func TestListDirtyFindsTheWalkersWorkLive(t *testing.T) {
	ctx, db := openDatabase(t)
	const extractor = 3

	seedVersions(ctx, t, db, map[string]int64{
		"r-clean": 5, "r-nostate": 1, "r-moved": 6, "r-oldext": 5, "r-newext": 5,
		"r-failed-exact": 5, "r-failonly": 2, "r-skip": 2, "r-skip-oldext": 2,
	})
	for _, state := range []struct {
		repo                 string
		contentVersion       any
		extractorVersion     int
		rowCount             any
		failures             int
		lastFailureExtractor any
	}{
		{"r-clean", 5, extractor, 2, 0, nil},
		{"r-moved", 5, extractor, 2, 0, nil},
		{"r-oldext", 5, extractor - 1, 2, 0, nil},
		{"r-newext", 5, extractor + 1, 2, 0, nil},
		{"r-failed-exact", 5, extractor, 2, 1, extractor},
		{"r-failonly", nil, extractor, nil, 2, extractor},
		{"r-skip", nil, extractor, nil, 5, extractor},
		{"r-skip-oldext", nil, extractor, nil, 5, extractor - 1},
		{"r-state-without-version", 1, extractor, 0, 0, nil},
	} {
		if _, err := db.ExecContext(ctx, `
INSERT INTO config_artifact_repo_state
    (repo_id, content_version, extractor_version, row_count, failure_count, last_failure_extractor_version)
VALUES ($1, $2, $3, $4, $5, $6)`,
			state.repo, state.contentVersion, state.extractorVersion, state.rowCount,
			state.failures, state.lastFailureExtractor); err != nil {
			t.Fatalf("seed state %s: %v", state.repo, err)
		}
	}

	got, err := artifacts.ListDirty(ctx, postgres.SQLDB{DB: db}, artifacts.DirtyRequest{
		ExtractorVersion: extractor, SkipAfterFailures: 5, Limit: 100,
	})
	if err != nil {
		t.Fatalf("ListDirty() error = %v", err)
	}
	want := []artifacts.Dirty{
		{RepoID: "r-failed-exact", Reason: artifacts.ReasonFailed},
		{RepoID: "r-failonly", Reason: artifacts.ReasonFailed},
		{RepoID: "r-skip-oldext", Reason: artifacts.ReasonFailed},
		{RepoID: "r-oldext", Reason: artifacts.ReasonStaleExtractor},
		{RepoID: "r-nostate", Reason: artifacts.ReasonMissing},
		{RepoID: "r-moved", Reason: artifacts.ReasonStaleVersion},
		{RepoID: "r-skip", Reason: artifacts.ReasonFailed, Skipped: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListDirty() =\n  %+v\nwant\n  %+v", got, want)
	}

	limited, err := artifacts.ListDirty(ctx, postgres.SQLDB{DB: db}, artifacts.DirtyRequest{
		ExtractorVersion: extractor, SkipAfterFailures: 5, Limit: 2,
	})
	if err != nil {
		t.Fatalf("ListDirty(limit 2) error = %v", err)
	}
	if !reflect.DeepEqual(limited, want[:2]) {
		t.Fatalf("ListDirty(limit 2) = %+v, want %+v", limited, want[:2])
	}
	if _, err := artifacts.ListDirty(ctx, postgres.SQLDB{DB: db}, artifacts.DirtyRequest{
		ExtractorVersion: extractor, SkipAfterFailures: 5,
	}); err == nil {
		t.Fatal("ListDirty(limit 0) error = nil, want a refusal")
	}
}
