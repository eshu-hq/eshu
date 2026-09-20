// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// neo4jFloorStatement is the operator-facing requirement the Neo4j
// call-chain statement imposes. Scoped CALL (x) { } subqueries arrived in
// Neo4j 5.23 and GQL SHORTEST in 5.21, so 5.23 is the floor.
const neo4jFloorStatement = "Neo4j 5.23 or later"

// TestNeo4jCallChainVersionFloorIsInOperatorDocs keeps the install docs
// honest about the Neo4j version the compat call-chain builder needs. Below
// the floor every call-chain request fails with a Cypher syntax error, so an
// operator must learn the requirement from the install page, not from a 500.
func TestNeo4jCallChainVersionFloorIsInOperatorDocs(t *testing.T) {
	t.Parallel()

	cypher, _ := BuildCallChainCypher(
		Request{Start: "a", End: "b", MaxDepth: 2},
		querycontract.GraphBackendNeo4j,
		querycontract.RepositoryAccessFilter{AllScopes: true},
	)
	normalized := querycontract.NormalizeCypherWhitespace(cypher)
	if !strings.Contains(normalized, "CALL (start, end) {") && !strings.Contains(normalized, "SHORTEST ") {
		t.Fatalf("the Neo4j builder no longer uses scoped CALL or GQL SHORTEST; revisit the documented %q floor and this guard:\n%s", neo4jFloorStatement, cypher)
	}

	path := filepath.Join("..", "..", "..", "..", "..", "docs", "public", "reference", "graph-backend-installation.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	docs := strings.Join(strings.Fields(string(raw)), " ")
	if !strings.Contains(docs, neo4jFloorStatement) {
		t.Fatalf("%s does not state %q, the floor the Neo4j call-chain statement needs", path, neo4jFloorStatement)
	}
}
