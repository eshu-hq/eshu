// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// rightAnchoredRepository matches a pattern whose only bound Repository sits at
// the arrow head, e.g. (source:Repository)-[rel]->(r:Repository {id: $repo_id}).
var rightAnchoredRepository = regexp.MustCompile(`\]->\(\w+:Repository \{id: \$repo_id\}\)`)

// TestRepositoryContextIncomingReadsAnchorOnTheBoundRepository guards #6794.
// Timing suggests NornicDB plans a relationship pattern from its left node: a
// right-anchored incoming read, (source:Repository)-[rel]->(r:Repository {id: $repo_id}),
// cost time that grew with graph size rather than with the bound repository's
// degree (the engine source was not read; see the #6794 evidence note). On a
// production-scale instance the incoming overview and consumers reads ran past
// the 10s API deadline and /context rendered no incoming rows. The same pattern
// written from the bound node, (r:Repository {id: $repo_id})<-[rel]-(source:Repository),
// returned the same rows in milliseconds.
func TestRepositoryContextIncomingReadsAnchorOnTheBoundRepository(t *testing.T) {
	t.Parallel()

	var cyphers []string
	reader := graph.FakeRepoGraphReader{
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			cyphers = append(cyphers, cypher)
			return nil, nil
		},
	}
	params := map[string]any{"repo_id": "repo-1"}
	ctx := context.Background()
	queryRepoRelationshipOverview(ctx, reader, params)
	queryRepoConsumers(ctx, reader, params)
	queryRepoDeployableUnitRelationshipOverview(ctx, reader, params)
	if _, err := QueryRepoDeploymentEvidence(ctx, reader, nil, params); err != nil {
		t.Fatalf("QueryRepoDeploymentEvidence: %v", err)
	}

	if got, want := len(cyphers), 7; got != want {
		t.Fatalf("graph reads = %d, want %d", got, want)
	}
	incoming := 0
	for _, cypher := range cyphers {
		if rightAnchoredRepository.MatchString(cypher) {
			t.Fatalf("incoming read is right-anchored on the bound repository:\n%s", cypher)
		}
		if !strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
			t.Fatalf("read does not start from the bound repository:\n%s", cypher)
		}
		if strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})<-[") {
			incoming++
		}
	}
	if got, want := incoming, 4; got != want {
		t.Fatalf("left-anchored incoming reads = %d, want %d", got, want)
	}
}

// TestRepositoryContextIncomingReadsKeepTheirRowOrder pins the ORDER BY each
// incoming read had before the #6794 anchor rewrite, so the rewrite changes the
// plan and not the response order.
func TestRepositoryContextIncomingReadsKeepTheirRowOrder(t *testing.T) {
	t.Parallel()

	var cyphers []string
	reader := graph.FakeRepoGraphReader{
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "<-[") {
				cyphers = append(cyphers, cypher)
			}
			return nil, nil
		},
	}
	params := map[string]any{"repo_id": "repo-1"}
	ctx := context.Background()
	queryRepoRelationshipOverview(ctx, reader, params)
	queryRepoConsumers(ctx, reader, params)
	queryRepoDeployableUnitRelationshipOverview(ctx, reader, params)

	want := []string{"ORDER BY type, source_name", "ORDER BY consumer_name", "ORDER BY source_name"}
	if len(cyphers) != len(want) {
		t.Fatalf("incoming reads = %d, want %d", len(cyphers), len(want))
	}
	for i, order := range want {
		if !strings.Contains(cyphers[i], order) {
			t.Fatalf("incoming read %d missing %q:\n%s", i, order, cyphers[i])
		}
	}
}
