// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7126 disposable PostgreSQL 18 proof of the config-artifact walker's dirty join: never-derived (failed) and retry repositories and old extractor versions first, then missing state, then moved content, skipped repositories last; exact rows and every newer-extractor row (exact, moved, failing) excluded; and over the cross product of the state inputs every listed repository carries Classify's label (retry excepted) and every untrusted repository in the walk universe is listed; enrolled in the live-postgres-readiness runner; nine proofs took 0.88-1.15s each and 9.1s for the package (disposable database plus full bootstrap per test) on a local PostgreSQL 18.6 on 2026-10-11; wall time is logged, not gated
//eshu:readiness:env ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/config/artifacts"
)

// TestListDirtyFindsTheWalkersWorkLive seeds one repository per dirty-join
// case and checks the walker's work list (protocol sections 1 and 4): never
// derived (failed), then a failed derive after a success (retry), then old
// extractor versions, then repositories with no state row, then moved content;
// a repository over the failure limit for the current extractor is listed
// last and marked skipped. Exact rows, every row from a newer extractor (exact,
// moved, or failing), and a state row with no version row are not work: the
// walker leaves newer-extractor rows to newer replicas (see dirtySQL for why).
func TestListDirtyFindsTheWalkersWorkLive(t *testing.T) {
	ctx, db := openDatabase(t)
	const extractor = 3

	seedVersions(ctx, t, db, map[string]int64{
		"r-clean": 5, "r-nostate": 1, "r-moved": 6, "r-oldext": 5, "r-newext": 5,
		"r-failed-exact": 5, "r-failonly": 2, "r-skip": 2, "r-skip-oldext": 2,
		"r-newext-moved": 6, "r-newext-failed": 5, "r-newext-failonly": 2,
		"r-oldext-moved": 6, "r-cvnull-counted": 5,
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
		{"r-newext-moved", 5, extractor + 1, 2, 0, nil},
		{"r-newext-failed", 5, extractor + 1, 2, 1, extractor + 1},
		{"r-newext-failonly", nil, extractor + 1, nil, 2, extractor + 1},
		{"r-oldext-moved", 5, extractor - 1, 2, 0, nil},
		{"r-cvnull-counted", nil, extractor, 2, 0, nil},
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
		{RepoID: "r-cvnull-counted", Reason: artifacts.ReasonFailed},
		{RepoID: "r-failonly", Reason: artifacts.ReasonFailed},
		{RepoID: "r-skip-oldext", Reason: artifacts.ReasonFailed},
		{RepoID: "r-failed-exact", Reason: artifacts.ReasonRetry},
		{RepoID: "r-oldext", Reason: artifacts.ReasonStaleExtractor},
		// Older extractor and moved content: labelled as Classify labels it,
		// ranked with the older extractors.
		{RepoID: "r-oldext-moved", Reason: artifacts.ReasonStaleVersion},
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

// TestListDirtyAgreesWithClassifyLive seeds the cross product of the state
// inputs (version row present or absent; no state row, or content_version
// NULL, equal or stale; row_count NULL or set; extractor_version older,
// current or newer; failure_count 0 or 1) and checks the dirty join against
// Classify. Wherever ListDirty returns a repository, its reason is the one
// Classify gives, except retry: the dirty join's own label for a failed
// derive whose rows Classify still trusts or finds stale only by extractor.
// It also checks the dirty join is wider than the reader: every repository in
// the walk universe that Classify does not trust is listed, unless its state
// row comes from a newer extractor, which the walker leaves alone.
func TestListDirtyAgreesWithClassifyLive(t *testing.T) {
	ctx, db := openDatabase(t)
	const (
		extractor = 3
		current   = int64(5)
	)
	type seeded struct {
		state   *artifacts.State
		version *int64
	}
	cases := map[string]seeded{}
	contentVersions := map[string]*int64{"null": nil, "equal": ptr(current), "stale": ptr(current - 1)}
	rowCounts := map[string]*int{"null": nil, "set": ptr(7)}
	extractors := map[string]int{"older": extractor - 1, "current": extractor, "newer": extractor + 1}
	for _, hasVersion := range []bool{true, false} {
		var version *int64
		if hasVersion {
			version = ptr(current)
		}
		cases[fmt.Sprintf("v%t-nostate", hasVersion)] = seeded{version: version}
		for cvName, cv := range contentVersions {
			for rcName, rc := range rowCounts {
				for evName, ev := range extractors {
					for _, failures := range []int{0, 1} {
						name := fmt.Sprintf("v%t-cv%s-rc%s-ev%s-f%d", hasVersion, cvName, rcName, evName, failures)
						cases[name] = seeded{version: version, state: &artifacts.State{
							ContentVersion: cv, RowCount: rc, ExtractorVersion: ev, FailureCount: failures,
						}}
					}
				}
			}
		}
	}
	for repo, c := range cases {
		if c.version != nil {
			seedVersions(ctx, t, db, map[string]int64{repo: *c.version})
		}
		if c.state == nil {
			continue
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO config_artifact_repo_state
    (repo_id, content_version, extractor_version, row_count, failure_count, last_failure_extractor_version)
VALUES ($1, $2, $3, $4, $5, NULL)`,
			repo, c.state.ContentVersion, c.state.ExtractorVersion, c.state.RowCount, c.state.FailureCount); err != nil {
			t.Fatalf("seed state %s: %v", repo, err)
		}
	}

	dirty, err := artifacts.ListDirty(ctx, postgres.SQLDB{DB: db}, artifacts.DirtyRequest{
		ExtractorVersion: extractor, SkipAfterFailures: 5, Limit: 1000,
	})
	if err != nil {
		t.Fatalf("ListDirty() error = %v", err)
	}
	listed := map[string]artifacts.Reason{}
	for _, d := range dirty {
		listed[d.RepoID] = d.Reason
	}
	for repo, c := range cases {
		want := artifacts.Classify(c.state, c.version, extractor)
		got, ok := listed[repo]
		newer := c.state != nil && c.state.ExtractorVersion > extractor
		switch {
		case !ok:
			if c.version != nil && !newer && want != artifacts.ReasonTrusted {
				t.Errorf("%s: not listed, but Classify = %q", repo, want)
			}
		case c.version == nil || newer:
			t.Errorf("%s: listed as %q, but it is outside the walker's work", repo, got)
		case got == artifacts.ReasonRetry:
			if c.state == nil || c.state.FailureCount == 0 ||
				(want != artifacts.ReasonTrusted && want != artifacts.ReasonStaleExtractor) {
				t.Errorf("%s: listed as retry, Classify = %q", repo, want)
			}
		case got != want:
			t.Errorf("%s: ListDirty reason %q, Classify %q", repo, got, want)
		}
	}
}

func ptr[T any](v T) *T { return &v }
