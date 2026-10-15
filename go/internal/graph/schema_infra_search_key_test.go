// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// infraSearchKeyIndexStatement is the exact #7245 DDL the ruling fixed: one
// keyword FULLTEXT index over the key and the string overflow flag of the 27
// infra search labels, strongly consistent.
const infraSearchKeyIndexStatement = "CREATE FULLTEXT INDEX infra_search_key_ft IF NOT EXISTS " +
	"FOR (n:CloudResource|K8sResource|KustomizeOverlay|TerraformResource|TerraformStateResource|" +
	"TerraformModule|TerraformVariable|TerraformOutput|TerraformDataSource|TerraformProvider|" +
	"TerraformLocal|TerraformBackend|TerraformImport|TerraformMovedBlock|TerraformRemovedBlock|" +
	"TerraformCheck|TerraformLockProvider|TerraformBlock|TerragruntConfig|TerragruntDependency|" +
	"CloudFormationResource|ArgoCDApplication|ArgoCDApplicationSet|CrossplaneXRD|" +
	"CrossplaneComposition|HelmChart|HelmValues) " +
	"ON EACH [n.infra_search_key, n.infra_search_ovf] " +
	"OPTIONS {indexConfig: {`fulltext.analyzer`: 'keyword', `fulltext.eventually_consistent`: false}}"

// TestSchemaInfraSearchKeyIndexIsNeo4jOnly pins the #7245 DDL: Neo4j lists
// and executes exactly the one FULLTEXT statement, NornicDB neither lists nor
// executes anything that mentions the key or flag, so the NornicDB statement
// list (and fingerprint) stays what it was.
func TestSchemaInfraSearchKeyIndexIsNeo4jOnly(t *testing.T) {
	t.Parallel()

	if got := len(InfraSearchKeyLabels()); got != 27 {
		t.Fatalf("InfraSearchKeyLabels() has %d labels, want 27", got)
	}
	for _, tc := range []struct {
		backend SchemaBackend
		want    int
	}{{SchemaBackendNeo4j, 1}, {SchemaBackendNornicDB, 0}} {
		listed, err := SchemaStatementsForBackend(tc.backend)
		if err != nil {
			t.Fatalf("SchemaStatementsForBackend(%s) error = %v", tc.backend, err)
		}
		executor := &schemaRecordingExecutor{}
		if err := EnsureSchemaWithBackend(context.Background(), executor, nil, tc.backend); err != nil {
			t.Fatalf("EnsureSchemaWithBackend(%s) error = %v", tc.backend, err)
		}
		executed := make([]string, 0, len(executor.calls))
		for _, call := range executor.calls {
			executed = append(executed, call.Cypher)
		}
		for name, stmts := range map[string][]string{"listed": listed, "executed": executed} {
			mentions := 0
			for _, stmt := range stmts {
				if strings.Contains(stmt, InfraSearchKeyProperty) || strings.Contains(stmt, InfraSearchOverflowProperty) {
					mentions++
					if stmt != infraSearchKeyIndexStatement {
						t.Errorf("%s %s statement = %q, want the ruled FULLTEXT DDL", tc.backend, name, stmt)
					}
				}
			}
			if mentions != tc.want {
				t.Errorf("%s %s %d infra search key statements, want %d", tc.backend, name, mentions, tc.want)
			}
		}
	}
	if slices.ContainsFunc(schemaFulltextIndexes, func(ft fulltextIndex) bool {
		return strings.Contains(ft.fallback, InfraSearchKeyProperty) || strings.Contains(ft.primary, InfraSearchKeyProperty)
	}) {
		t.Fatal("the infra search key index is in schemaFulltextIndexes; NornicDB would receive its procedure form")
	}
}

// TestSchemaIndexKeysExcludeInfraSearchKeyIndex proves the RANGE key guard
// never measures the key or the flag: the FULLTEXT index tolerates the
// 32,000-byte keys the clause stores, and if the guard read it as a RANGE key
// it would drop every node whose key passes MaxIndexKeyBytes.
func TestSchemaIndexKeysExcludeInfraSearchKeyIndex(t *testing.T) {
	t.Parallel()

	keys, err := SchemaIndexKeys()
	if err != nil {
		t.Fatalf("SchemaIndexKeys() error = %v", err)
	}
	for _, key := range keys {
		if slices.Contains(key.Properties, InfraSearchKeyProperty) || slices.Contains(key.Properties, InfraSearchOverflowProperty) {
			t.Errorf("SchemaIndexKeys() includes %+v; the infra search key index must be excluded from the RANGE guard", key)
		}
	}
	if !isFulltextSchemaStatement(infraSearchKeyIndexStatement) {
		t.Fatal("isFulltextSchemaStatement(infra search key DDL) = false, want true")
	}
}

// TestGuardIndexKeyWritesKeepsOversizedInfraSearchKey is the behavioral half
// of the exclusion: a 40,000-byte value written to infra_search_key on an
// infra label is not a RANGE key, so the guard keeps the row. (The writer
// clause, not this guard, keeps the key under the term limit.)
func TestGuardIndexKeyWritesKeepsOversizedInfraSearchKey(t *testing.T) {
	t.Parallel()

	cypher := "UNWIND $rows AS row\nMERGE (n:CloudResource {uid: row.uid})\nSET n.infra_search_key = row.k"
	rows := []map[string]any{{"uid": "u1", "k": strings.Repeat("k", 40000)}}
	out, dropped, skip := GuardIndexKeyWrites(cypher, map[string]any{"rows": rows})
	if skip || len(dropped) != 0 {
		t.Fatalf("GuardIndexKeyWrites dropped %+v skip=%v; infra_search_key is not a RANGE key", dropped, skip)
	}
	if got := out["rows"].([]map[string]any); len(got) != 1 {
		t.Fatalf("kept rows = %d, want 1", len(got))
	}
}

// TestIsInfraSearchKeyLabel covers membership, a non-member, and copy
// semantics.
func TestIsInfraSearchKeyLabel(t *testing.T) {
	t.Parallel()

	if !IsInfraSearchKeyLabel("TerraformVariable") {
		t.Fatal("IsInfraSearchKeyLabel(TerraformVariable) = false, want true")
	}
	if IsInfraSearchKeyLabel("Function") {
		t.Fatal("IsInfraSearchKeyLabel(Function) = true, want false")
	}
	labels := InfraSearchKeyLabels()
	labels[0] = "Mutated"
	if InfraSearchKeyLabels()[0] == "Mutated" {
		t.Fatal("InfraSearchKeyLabels() returned the shared slice; want a copy")
	}
}
