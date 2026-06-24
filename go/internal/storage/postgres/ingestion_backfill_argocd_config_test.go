// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/lib/pq"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestLoadArgoCDGeneratorConfigFactsPassesRepoIDsAndScans(t *testing.T) {
	t.Parallel()

	payload := `{"repo_id":"repo:gitops-config","content_path":"apps/payments/config.yaml","content_body":"service: payments-api"}`
	queryer := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{contentFactRow("cfg-1", "scope-cfg", "gen-cfg", "content", payload)}},
		},
	}

	loaded, err := loadArgoCDGeneratorConfigFacts(
		context.Background(),
		queryer,
		[]string{"repo:gitops-config"},
	)
	if err != nil {
		t.Fatalf("loadArgoCDGeneratorConfigFacts returned error: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded %d facts, want 1", len(loaded))
	}
	if loaded[0].FactKind != "content" {
		t.Fatalf("loaded fact kind = %q, want content", loaded[0].FactKind)
	}

	if len(queryer.queries) != 1 {
		t.Fatalf("issued %d queries, want 1", len(queryer.queries))
	}
	call := queryer.queries[0]
	if call.query != listArgoCDGeneratorConfigFactRecordsQuery {
		t.Fatalf("unexpected query:\n%s", call.query)
	}
	if len(call.args) != 1 {
		t.Fatalf("query args = %d, want 1", len(call.args))
	}
	repoArgs, ok := call.args[0].(pq.StringArray)
	if !ok {
		t.Fatalf("query arg type = %T, want pq.StringArray", call.args[0])
	}
	if len(repoArgs) != 1 || repoArgs[0] != "repo:gitops-config" {
		t.Fatalf("repo args = %v, want [repo:gitops-config]", []string(repoArgs))
	}
}

func TestLoadArgoCDGeneratorConfigFactsEmptyReposShortCircuits(t *testing.T) {
	t.Parallel()

	queryer := &fakeExecQueryer{}
	loaded, err := loadArgoCDGeneratorConfigFacts(context.Background(), queryer, nil)
	if err != nil {
		t.Fatalf("expected nil error for empty repos, got %v", err)
	}
	if loaded != nil {
		t.Fatalf("expected nil facts for empty repos, got %v", loaded)
	}
	if len(queryer.queries) != 0 {
		t.Fatalf("expected no queries for empty repos, got %d", len(queryer.queries))
	}
}

func TestArgoCDGeneratorConfigQueryShape(t *testing.T) {
	t.Parallel()

	query := listArgoCDGeneratorConfigFactRecordsQuery
	for _, want := range []string{
		"fact.fact_kind IN ('content', 'file')",
		"payload->>'repo_id'",
		"= ANY($1)",
		"latest_generations",
		".yaml",
		".yml",
		".json",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("argocd config query missing %q", want)
		}
	}
}

func TestMergeRelationshipFactsDedupesByFactID(t *testing.T) {
	t.Parallel()

	primary := []facts.Envelope{
		{FactID: "fact-a"},
		{FactID: "fact-b"},
	}
	secondary := []facts.Envelope{
		{FactID: "fact-b"}, // duplicate already present in primary
		{FactID: "fact-c"},
	}

	merged := mergeRelationshipFacts(primary, secondary)
	ids := make([]string, 0, len(merged))
	for _, envelope := range merged {
		ids = append(ids, envelope.FactID)
	}
	if got, want := strings.Join(ids, ","), "fact-a,fact-b,fact-c"; got != want {
		t.Fatalf("merged fact IDs = %q, want %q", got, want)
	}
}

func TestMergeRelationshipFactsEmptySecondaryReturnsPrimary(t *testing.T) {
	t.Parallel()

	primary := []facts.Envelope{{FactID: "fact-a"}}
	merged := mergeRelationshipFacts(primary, nil)
	if len(merged) != 1 || merged[0].FactID != "fact-a" {
		t.Fatalf("merged = %v, want [fact-a]", merged)
	}
}
