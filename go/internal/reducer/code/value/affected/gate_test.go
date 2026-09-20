// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package affected

import (
	"context"
	"strings"
	"testing"
)

// fakeRunner replays canned rows per statement.
type fakeRunner struct {
	rows map[string][]map[string]any
	err  map[string]error
	seen []string
}

func (f *fakeRunner) Run(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
	f.seen = append(f.seen, cypher)
	if err, ok := f.err[cypher]; ok {
		return nil, err
	}
	return f.rows[cypher], nil
}

func repoRow(repo string) map[string]any {
	return map[string]any{"repo_id": repo}
}

func TestReposWithCloudCallersCountsDistinctRepos(t *testing.T) {
	t.Parallel()

	g := &fakeRunner{rows: map[string][]map[string]any{
		reposWithCloudCallersCypher: {repoRow("r1"), repoRow("r1"), repoRow("r2")},
	}}
	n, err := ReposWithCloudCallers(context.Background(), g, []string{"r1", "r2", "r3"})
	if err != nil {
		t.Fatalf("ReposWithCloudCallers() error = %v", err)
	}
	if n != 2 {
		t.Errorf("ReposWithCloudCallers() = %d, want 2 distinct repos", n)
	}
}

func TestReposWithCloudCallersEmptyKeysSkipsQuery(t *testing.T) {
	t.Parallel()

	g := &fakeRunner{}
	n, err := ReposWithCloudCallers(context.Background(), g, nil)
	if err != nil {
		t.Fatalf("ReposWithCloudCallers() error = %v", err)
	}
	if n != 0 {
		t.Errorf("ReposWithCloudCallers() = %d, want 0 without a query", n)
	}
	if len(g.seen) != 0 {
		t.Errorf("ran %d queries, want none for empty keys", len(g.seen))
	}
}

func TestReposWithCloudCallersForWorkloads(t *testing.T) {
	t.Parallel()

	g := &fakeRunner{rows: map[string][]map[string]any{
		workloadReposWithCloudCallersCypher: {repoRow("r9")},
	}}
	n, err := ReposWithCloudCallersForWorkloads(context.Background(), g, []string{"w1"})
	if err != nil {
		t.Fatalf("ReposWithCloudCallersForWorkloads() error = %v", err)
	}
	if n != 1 {
		t.Errorf("ReposWithCloudCallersForWorkloads() = %d, want 1", n)
	}
}

func TestReposWithCloudCallersForPrincipals(t *testing.T) {
	t.Parallel()

	g := &fakeRunner{rows: map[string][]map[string]any{
		principalReposWithCloudCallersCypher: {},
	}}
	n, err := ReposWithCloudCallersForPrincipals(context.Background(), g, []string{"p1"})
	if err != nil {
		t.Fatalf("ReposWithCloudCallersForPrincipals() error = %v", err)
	}
	if n != 0 {
		t.Errorf("ReposWithCloudCallersForPrincipals() = %d, want 0", n)
	}
}

func TestReposWithCloudCallersForResources(t *testing.T) {
	t.Parallel()

	g := &fakeRunner{rows: map[string][]map[string]any{
		principalReposWithCloudCallersCypher: {repoRow("r7")},
	}}
	n, err := ReposWithCloudCallersForResources(context.Background(), g, []string{"res-1"})
	if err != nil {
		t.Fatalf("ReposWithCloudCallersForResources() error = %v", err)
	}
	if n != 1 {
		t.Errorf("ReposWithCloudCallersForResources() = %d, want 1", n)
	}
	if len(g.seen) != 1 || g.seen[0] != principalReposWithCloudCallersCypher {
		t.Errorf("ran statements %v, want exactly the shared principal statement", g.seen)
	}
}

func TestGateStatementsAvoidMisansweredShapes(t *testing.T) {
	t.Parallel()

	for name, stmt := range map[string]string{
		"repos":     reposWithCloudCallersCypher,
		"workload":  workloadReposWithCloudCallersCypher,
		"principal": principalReposWithCloudCallersCypher,
	} {
		for _, banned := range []string{"collect(", "DISTINCT", "size(", "[0]", "WITH ", "OPTIONAL MATCH", "*.."} {
			if strings.Contains(stmt, banned) {
				t.Errorf("%s statement contains %q", name, banned)
			}
		}
		if !strings.HasPrefix(stmt, "UNWIND ") {
			t.Errorf("%s statement must start UNWIND-first", name)
		}
	}
}
