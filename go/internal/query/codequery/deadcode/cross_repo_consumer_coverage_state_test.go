// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode_test

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/code"
)

// coverageGapObjects returns consumer_coverage.incomplete as decoded objects.
func coverageGapObjects(t *testing.T, coverage map[string]any) []map[string]any {
	t.Helper()

	raw, ok := coverage["incomplete"].([]any)
	if !ok {
		t.Fatalf("consumer_coverage.incomplete = %#v, want an array", coverage["incomplete"])
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		out = append(out, item.(map[string]any))
	}
	return out
}

// Every incomplete consumer is reported with the reason, the generation being
// waited for and whether waiting can fix it, and the legacy id list stays in
// step with it (#7547).
func TestCrossRepoDeadCodeConsumerCoverageReportsPerRepositoryState(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		body string
		auth *auth.AuthContext
	}{
		"named":    {body: `{"repo_id":"repo-producer","consumer_repo_ids":["repo-consumer","repo-other"],"limit":10}`},
		"unscoped": {body: `{"repo_id":"repo-producer","limit":10}`},
		"grant": {
			body: `{"repo_id":"repo-producer","limit":10}`,
			auth: &auth.AuthContext{Mode: auth.AuthModeScoped, AllowedRepositoryIDs: []string{"repo-producer", "repo-consumer", "repo-other"}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := coverageStore(nil)
			store.coverageDetail = []code.CrossRepoDeadCodeCoverageGap{
				{RepositoryID: "repo-consumer", State: "older_epoch", GenerationID: "gen-9", Retryable: true},
				{RepositoryID: "repo-other", State: "truncated", GenerationID: "gen-3"},
				{RepositoryID: "repo-zzz", State: "no_active_scope"},
			}
			status, data := postCoverageRequest(t, store, tc.body, tc.auth)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200", status)
			}
			coverage := coverageObject(t, data)
			want := []map[string]any{
				{"repository_id": "repo-consumer", "state": "older_epoch", "generation_id": "gen-9", "retryable": true},
				{"repository_id": "repo-other", "state": "truncated", "generation_id": "gen-3", "retryable": false},
				// No active scope: no generation to wait for, so the key is absent.
				{"repository_id": "repo-zzz", "state": "no_active_scope", "retryable": false},
			}
			if got := coverageGapObjects(t, coverage); !reflect.DeepEqual(got, want) {
				t.Fatalf("incomplete = %#v, want %#v", got, want)
			}
			assertQueryTestStringSliceEqual(t, coverage["incomplete_repo_ids"], []string{"repo-consumer", "repo-other", "repo-zzz"})
			if got := coverage["retryable"]; got != false {
				t.Fatalf("consumer_coverage.retryable = %v, want false: one gap is truncated", got)
			}
			// The symbol-level reason is unchanged.
			row := assertSymbolBucket(t, data, "unknown")
			assertCrossRepoDeadCodeReason(t, row, "consumer_coverage_incomplete")
		})
	}
}

// retryable tells an agent whether waiting can help: true only when every gap
// is known and retryable, and the list was not cut.
func TestCrossRepoDeadCodeConsumerCoverageRetryableOnlyWhenEveryGapIs(t *testing.T) {
	t.Parallel()

	retryable := []code.CrossRepoDeadCodeCoverageGap{
		{RepositoryID: "repo-consumer", State: "no_snapshot_yet", GenerationID: "gen-1", Retryable: true},
		{RepositoryID: "repo-other", State: "older_epoch", GenerationID: "gen-2", Retryable: true},
	}
	for name, tc := range map[string]struct {
		gaps []code.CrossRepoDeadCodeCoverageGap
		cut  bool
		want bool
	}{
		"every gap retryable":      {gaps: retryable, want: true},
		"a cut list hides gaps":    {gaps: retryable, cut: true, want: false},
		"one truncated gap":        {gaps: append([]code.CrossRepoDeadCodeCoverageGap{{RepositoryID: "repo-a", State: "truncated"}}, retryable...), want: false},
		"no active scope is final": {gaps: append([]code.CrossRepoDeadCodeCoverageGap{{RepositoryID: "repo-a", State: "no_active_scope"}}, retryable...), want: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := coverageStore(nil)
			store.coverageDetail = tc.gaps
			store.coverageCut = tc.cut
			status, data := postCoverageRequest(t, store, `{"repo_id":"repo-producer","limit":10}`, nil)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200", status)
			}
			coverage := coverageObject(t, data)
			if got := coverage["retryable"]; got != tc.want {
				t.Fatalf("consumer_coverage.retryable = %v, want %v", got, tc.want)
			}
			if got := coverage["incomplete_truncated"]; got != tc.cut {
				t.Fatalf("incomplete_truncated = %v, want %v", got, tc.cut)
			}
		})
	}
}

// A complete answer carries an empty (never null) incomplete array and is not
// retryable: there is nothing to wait for.
func TestCrossRepoDeadCodeCompleteCoverageHasEmptyIncompleteArray(t *testing.T) {
	t.Parallel()

	store := coverageStore(nil)
	status, data := postCoverageRequest(t, store,
		`{"repo_id":"repo-producer","consumer_repo_ids":["repo-consumer"],"limit":10}`, nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	coverage := coverageObject(t, data)
	if got := coverageGapObjects(t, coverage); len(got) != 0 {
		t.Fatalf("incomplete = %#v, want empty", got)
	}
	if got := coverage["retryable"]; got != false {
		t.Fatalf("consumer_coverage.retryable = %v, want false", got)
	}
}
