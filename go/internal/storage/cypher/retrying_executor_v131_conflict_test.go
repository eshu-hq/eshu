// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"errors"
	"fmt"
	"testing"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

func TestClassifyTransientNeo4jErrorRejectsV131ConflictNearMisses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "wrong typed code",
			err: &neo4jdriver.Neo4jError{
				Code: nornicDBStatementSyntaxErrorCode,
				Msg:  observedNornicDBV131WriteConflict,
			},
			want: "",
		},
		{
			name: "wrapped typed code",
			err: fmt.Errorf("phase group failed: %w", &neo4jdriver.Neo4jError{
				Code: nornicDBTransactionOutdatedCode,
				Msg:  observedNornicDBV131WriteConflict,
			}),
			want: graphWriteRetryReasonWriteConflict,
		},
		{
			name: "prefixed delimiter",
			err: &neo4jdriver.Neo4jError{
				Code: nornicDBTransactionOutdatedCode,
				Msg:  "relationship update failed: non-conflict detected: edge nornic:123 changed after transaction start",
			},
			want: graphWriteRetryReasonTransient,
		},
		{
			name: "missing subject",
			err: &neo4jdriver.Neo4jError{
				Code: nornicDBTransactionOutdatedCode,
				Msg:  "relationship update failed: conflict detected: nornic:123 changed after transaction start",
			},
			want: graphWriteRetryReasonTransient,
		},
		{
			name: "empty identifier",
			err: &neo4jdriver.Neo4jError{
				Code: nornicDBTransactionOutdatedCode,
				Msg:  "relationship update failed: conflict detected: edge  changed after transaction start",
			},
			want: graphWriteRetryReasonTransient,
		},
		{
			name: "prefixed suffix",
			err: &neo4jdriver.Neo4jError{
				Code: nornicDBTransactionOutdatedCode,
				Msg:  "relationship update failed: conflict detected: edge nornic:123 unchanged after transaction start",
			},
			want: graphWriteRetryReasonTransient,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyTransientNeo4jError(tc.err); got != tc.want {
				t.Fatalf("retry reason = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClassifyRetryableGraphWriteGroupErrorRejectsUntypedV131Conflict(t *testing.T) {
	t.Parallel()

	statements := []Statement{
		{Cypher: "MERGE (n:Node {id: $id})"},
		{Cypher: "CREATE (n:Audit {id: $id})"},
	}
	if got := classifyRetryableGraphWriteGroupError(
		errors.New(observedNornicDBV131WriteConflict),
		statements,
	); got != "" {
		t.Fatalf("retry reason = %q, want terminal mixed group", got)
	}
}
