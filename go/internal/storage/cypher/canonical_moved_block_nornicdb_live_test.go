// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

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

// nornicDBConstraintName returns the name a "CREATE CONSTRAINT <name> ..."
// statement declares.
func nornicDBConstraintName(t *testing.T, statement string) string {
	t.Helper()
	fields := strings.Fields(statement)
	if len(fields) < 3 || fields[0] != "CREATE" || fields[1] != "CONSTRAINT" {
		t.Fatalf("cannot derive a constraint name from %q", statement)
	}
	return fields[2]
}

// nornicDBConstraintNames returns the names SHOW CONSTRAINTS reports. It reads
// the whole listing because NornicDB ignores a YIELD/WHERE clause on SHOW. A row
// without a string name fails the test: skipping it would let an unreadable
// listing satisfy the absence check vacuously. Each call sends a unique comment
// because NornicDB answers a repeated identical SHOW CONSTRAINTS from its result
// cache, which would hand the pre-bootstrap listing back after the drop.
func nornicDBConstraintNames(ctx context.Context, t *testing.T, runner *boltRetractTestRunner) map[string]struct{} {
	t.Helper()
	rows, err := runner.runCypher(ctx, fmt.Sprintf("SHOW CONSTRAINTS /* %d */", time.Now().UnixNano()), nil)
	if err != nil {
		t.Fatalf("show constraints: %v", err)
	}
	names := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		name, ok := row["name"].(string)
		if !ok {
			t.Fatalf("SHOW CONSTRAINTS row has no string name: %v", row)
		}
		names[name] = struct{}{}
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
	// Positive control: the absence check below is vacuous unless the seeded
	// constraints are visible in the listing it reads.
	seeded := nornicDBConstraintNames(ctx, t, runner)
	for _, statement := range nornicDBLegacyPathConstraints {
		name := nornicDBConstraintName(t, statement)
		if _, ok := seeded[name]; !ok {
			t.Fatalf("seeded constraint %s not visible in SHOW CONSTRAINTS; cannot prove the bootstrap drops it", name)
		}
	}
	if err := graph.EnsureSchemaWithBackendStrict(ctx, boltSchemaExecutor{runner: runner}, nil, graph.SchemaBackendNornicDB); err != nil {
		t.Fatalf("apply nornicdb schema: %v", err)
	}
	// The bootstrap must drop the legacy constraints on an existing store.
	names := nornicDBConstraintNames(ctx, t, runner)
	for _, statement := range nornicDBLegacyPathConstraints {
		name := nornicDBConstraintName(t, statement)
		if _, ok := names[name]; ok {
			t.Errorf("constraint %s still exists after the schema bootstrap; want it dropped", name)
		}
	}

	writer := NewCanonicalNodeWriter(&boltTestExecutor{runner: runner}, 100, nil)
	runMovedBlockCases(t, runner, writer)
}
