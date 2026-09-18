// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestRelationshipsGraphRowAnchorsOnRepositoryForRepoIDBranch proves the
// name+repo_id branch of relationshipsGraphRow renders a repository-anchored
// MATCH instead of the backward multi-hop EXISTS filter issue #6786 defect 2
// found NornicDB v1.3.3 silently ignores. The anchor must bind $repo_id
// inside the MATCH pattern itself (REPO_CONTAINS -> CONTAINS -> e), matching
// entity.BuildResolveEntityGraphQuery's repository-anchored shape, and the
// rendered statement must carry no EXISTS block at all.
func TestRelationshipsGraphRowAnchorsOnRepositoryForRepoIDBranch(t *testing.T) {
	t.Parallel()

	var capturedCypher string
	var capturedParams map[string]any
	reader := querytestutil.FakeGraphReaderWithSingle{
		RunSingleFn: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
			capturedCypher = cypher
			capturedParams = params
			return map[string]any{"id": "fn:run-a", "name": "Run", "repo_id": "repository:a"}, nil
		},
	}
	handler := &CodeHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative}

	row, err := handler.relationshipsGraphRow(context.Background(), "", "Run", "repository:a", "", "")
	if err != nil {
		t.Fatalf("relationshipsGraphRow() error = %v", err)
	}
	if row == nil {
		t.Fatal("relationshipsGraphRow() = nil row, want the fake's row")
	}

	if strings.Contains(capturedCypher, "EXISTS") {
		t.Fatalf("repo_id branch must not render an EXISTS filter:\n%s", capturedCypher)
	}
	if !strings.Contains(capturedCypher, "MATCH (anchorRepo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(anchorFile:File)-[:CONTAINS]->(e)") {
		t.Fatalf("repo_id branch does not anchor the scan on the repository:\n%s", capturedCypher)
	}
	if !strings.Contains(capturedCypher, "WHERE e.name = $name") {
		t.Fatalf("repo_id branch lost the name predicate:\n%s", capturedCypher)
	}
	if got, want := capturedParams["repo_id"], "repository:a"; got != want {
		t.Errorf("repo_id param = %#v, want %#v", got, want)
	}
	if got, want := capturedParams["name"], "Run"; got != want {
		t.Errorf("name param = %#v, want %#v", got, want)
	}
}
