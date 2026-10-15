// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membershipstore_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/membership"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
	membershipstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/membership"
)

// TestObservationStoreKnownScopesReadsTheGroupPrefixPartition proves the
// gitlab_group known-scope read binds the lowercased owner plus a trailing
// slash with the lowercased host, and runs the prefix query: starts_with
// instead of LIKE because _ is a LIKE wildcard and legal in GitLab paths,
// and the trailing slash keeps acme/platform from matching acme/platformer.
func TestObservationStoreKnownScopesReadsTheGroupPrefixPartition(t *testing.T) {
	t.Parallel()

	database := &fake.ExecQueryer{QueryResponses: []fake.Rows{{Data: [][]any{
		{"git-repository-scope:a", "acme/platform/a"},
		{"git-repository-scope:b", "acme/platform/sub/b"},
	}}}}
	got, err := membershipstore.NewObservationStore(database).KnownScopes(context.Background(), membership.Partition{
		Owner: " Acme/Platform ", Host: " GitLab.com ", PathPrefix: true,
	})
	if err != nil {
		t.Fatalf("KnownScopes() error = %v", err)
	}
	want := []membership.KnownScope{
		{ScopeID: "git-repository-scope:a", Slug: "acme/platform/a"},
		{ScopeID: "git-repository-scope:b", Slug: "acme/platform/sub/b"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("KnownScopes() = %+v, want %+v", got, want)
	}
	query := database.Queries[0]
	if !reflect.DeepEqual(query.Args, []any{"acme/platform/", "gitlab.com"}) {
		t.Fatalf("bound args = %#v, want the prefix owner and the lowercased host", query.Args)
	}
	if query.Query != membershipstore.KnownScopesPrefixQuery {
		t.Fatalf("prefix query differs from the exported constant:\nrun:\n%s\nconst:\n%s", query.Query, membershipstore.KnownScopesPrefixQuery)
	}
	prefixSQL := membershipstore.KnownScopesPrefixQuery
	for _, want := range []string{
		"source_system = 'git'",
		"scope_kind = 'repository'",
		"collector_kind = 'git'",
		"starts_with(lower(payload->>'repo_slug'), $1)",
		"($2 = '' OR lower(split_part(payload->>'remote_url', '/', 3)) = $2)",
	} {
		if !strings.Contains(prefixSQL, want) {
			t.Fatalf("prefix query missing %q: %s", want, prefixSQL)
		}
	}
	if strings.Contains(prefixSQL, "LIKE") {
		t.Fatalf("prefix query must not use LIKE (_ is a wildcard and legal in GitLab paths): %s", prefixSQL)
	}
}

// TestObservationStoreUpsertAcceptsGitLabGroup proves validateBatch admits
// gitlab_group batches with the full row set, including not_listed rows.
func TestObservationStoreUpsertAcceptsGitLabGroup(t *testing.T) {
	t.Parallel()

	selector := membership.NewGitLabGroupSelector("gitlabGroup", "gitlab.com", "acme/platform", nil, false, "")
	database := &fake.ExecQueryer{}
	err := membershipstore.NewObservationStore(database).UpsertObservations(context.Background(), membership.Batch{
		Selector: selector, EvaluatedAt: evaluatedAt, LivenessWindow: testWindow,
		Rows: []membership.Row{
			{ScopeID: "git-repository-scope:a", State: membership.StateSelected, GitHubRepoID: 101},
			{ScopeID: "git-repository-scope:b", State: membership.StateNotListed},
			{ScopeID: "git-repository-scope:c", State: membership.StateRuleExcluded},
		},
	})
	if err != nil {
		t.Fatalf("UpsertObservations() error = %v, want nil", err)
	}
	if len(database.Execs) != 1 {
		t.Fatalf("exec count = %d, want 1", len(database.Execs))
	}
}
