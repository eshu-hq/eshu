// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

func TestReducerCypherExecutorRetriesTypedNornicDBPlatformCommitUniqueConflict(t *testing.T) {
	t.Parallel()

	session := &fakeNeo4jSession{errs: []error{
		&neo4jdriver.Neo4jError{
			Code: "Neo.ClientError.Statement.SyntaxError",
			Msg: "commit failed: constraint violation: " +
				"Constraint violation (UNIQUE on Platform.[id]): " +
				"Node with id=platform:kubernetes:none:prod:prod:none already exists",
		},
		nil,
	}}
	executor := newReducerCypherExecutor(session, nil)

	err := executor.ExecuteCypher(context.Background(), `UNWIND $rows AS row
MERGE (p:Platform {id: row.platform_id})
ON CREATE SET p.evidence_source = row.evidence_source
SET p.name = row.platform_name`, map[string]any{"rows": []map[string]any{{
		"platform_id": "platform:kubernetes:none:prod:prod:none",
	}}})
	if err != nil {
		t.Fatalf("ExecuteCypher() error = %v, want nil after retry", err)
	}
	if got, want := len(session.calls), 2; got != want {
		t.Fatalf("session calls = %d, want %d", got, want)
	}
}
