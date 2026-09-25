// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

// nornicDBLegacyPathConstraints are the single-property uniqueness constraints
// the NornicDB schema emitted before #7097. NornicDB never created the
// composite (name, path) forms, so only these three narrowed the uid identity.
// The test creates them first, as an already bootstrapped store has them, so
// applying the current schema must drop them.
var nornicDBLegacyPathConstraints = []string{
	"CREATE CONSTRAINT kustomize_unique IF NOT EXISTS FOR (ko:KustomizeOverlay) REQUIRE ko.path IS UNIQUE",
	"CREATE CONSTRAINT helm_values_unique IF NOT EXISTS FOR (hv:HelmValues) REQUIRE hv.path IS UNIQUE",
	"CREATE CONSTRAINT tg_config_unique IF NOT EXISTS FOR (tg:TerragruntConfig) REQUIRE tg.path IS UNIQUE",
}

// nornicDBConstraintNames returns the names SHOW CONSTRAINTS reports. It reads
// the whole listing because NornicDB ignores a YIELD/WHERE clause on SHOW.
func nornicDBConstraintNames(ctx context.Context, t *testing.T, runner *boltRetractTestRunner) map[string]struct{} {
	t.Helper()
	rows, err := runner.runCypher(ctx, "SHOW CONSTRAINTS", nil)
	if err != nil {
		t.Fatalf("show constraints: %v", err)
	}
	names := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if name, ok := row["name"].(string); ok {
			names[name] = struct{}{}
		}
	}
	return names
}

// TestLiveNornicDBMovedCanonicalBlockKeepsOneNode is the #7097 regression, the
// NornicDB peer of TestLiveNeo4jMovedCanonicalBlockKeepsOneNode (#7095). The
// canonical writer upserts a moved block's new uid before entity_retract
// deletes the prior generation's node, so a NornicDB uniqueness constraint on
// (path) alone -- helm_values_unique, kustomize_unique, tg_config_unique --
// rejected the delta with Neo.TransientError.Transaction.Outdated (UNIQUE
// constraint violation). NornicDB reports that as transient, so the work item
// retried instead of dead-lettering. It drives the real schema bootstrap over a
// store that already carries the legacy constraints, then the real
// CanonicalNodeWriter for both generations.
//
// Gate: ESHU_CYPHER_BOLT_DSN pointing at a NornicDB backend; the Neo4j
// database name (ESHU_CYPHER_BOLT_DATABASE=neo4j) skips it.
func TestLiveNornicDBMovedCanonicalBlockKeepsOneNode(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DATABASE")) == "neo4j" {
		t.Skip("ESHU_CYPHER_BOLT_DATABASE=neo4j; this proof is NornicDB-only")
	}
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()

	// Model a pre-#7097 store: the legacy path constraints already exist.
	for _, statement := range nornicDBLegacyPathConstraints {
		if err := boltWriteStatement(ctx, runner, statement, nil); err != nil {
			t.Fatalf("seed legacy constraint %q: %v", statement, err)
		}
	}
	if err := graph.EnsureSchemaWithBackendStrict(ctx, boltSchemaExecutor{runner: runner}, nil, graph.SchemaBackendNornicDB); err != nil {
		t.Fatalf("apply nornicdb schema: %v", err)
	}
	// The bootstrap must drop the legacy constraints on an existing store.
	names := nornicDBConstraintNames(ctx, t, runner)
	for _, statement := range nornicDBLegacyPathConstraints {
		name := strings.Fields(statement)[2]
		if _, ok := names[name]; ok {
			t.Errorf("constraint %s still exists after the schema bootstrap; want it dropped", name)
		}
	}

	writer := NewCanonicalNodeWriter(&boltTestExecutor{runner: runner}, 100, nil)
	runMovedBlockCases(t, runner, writer)
}
