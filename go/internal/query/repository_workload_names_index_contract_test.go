// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestRepositoryWorkloadNamesUsesScopeIndexPredicate(t *testing.T) {
	const migrationName = "fact_records_workload_names_scope_idx"
	var indexSQL string
	for _, definition := range storagepostgres.BootstrapDefinitions() {
		if definition.Name == migrationName {
			indexSQL = strings.Join(strings.Fields(definition.SQL), " ")
			break
		}
	}
	if indexSQL == "" {
		t.Fatalf("%s migration definition missing", migrationName)
	}
	if want := "ON fact_records (scope_id) WHERE fact_kind = 'reducer_workload_identity' AND is_tombstone = FALSE"; !strings.Contains(indexSQL, want) {
		t.Fatalf("migration predicate differs from workload-name query contract: %s", indexSQL)
	}

	querySQL := strings.Join(strings.Fields(repositoryWorkloadNamesSQL), " ")
	if want := "WHERE scope_id = $1 AND fact_kind = 'reducer_workload_identity' AND NOT is_tombstone ORDER BY entity_key"; !strings.Contains(querySQL, want) {
		t.Fatalf("workload-name query cannot use scope index predicate: %s", querySQL)
	}

	const scopeID = "scope-private-value"
	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns: []string{"entity_key"},
		rows:    [][]driver.Value{{"workload:api"}},
		queryContainsInOrder: []string{
			"FROM fact_records",
			"WHERE scope_id = $1",
			"AND fact_kind = 'reducer_workload_identity'",
			"AND NOT is_tombstone",
		},
		wantArgs: []driver.Value{scopeID},
	}})
	names, err := NewContentReader(db).repositoryWorkloadNames(context.Background(), scopeID)
	if err != nil {
		t.Fatalf("repositoryWorkloadNames() error = %v", err)
	}
	if len(names) != 1 || names[0] != "api" {
		t.Fatalf("repositoryWorkloadNames() = %v, want [api]", names)
	}
}
