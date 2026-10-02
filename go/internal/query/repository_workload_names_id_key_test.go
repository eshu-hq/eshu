// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"reflect"
	"testing"
)

// TestRepositoryWorkloadNamesResolveIdKeysToRepositoryName pins the #7384 Q2
// read-model contract: workload_identity follow-up keys are `workload:` plus
// the repository ID, so stripping the prefix would report an id
// (`repository:r_<hex>`) as a workload name. The read model must return the
// repository fact's payload name for the scope instead -- never a
// `repository:r_...` id -- and an empty (not fallback) result when the
// repository fact is tombstoned or absent.
func TestRepositoryWorkloadNamesResolveIdKeysToRepositoryName(t *testing.T) {
	t.Parallel()

	const scopeID = "git-repository-scope:repository:r_7384abcd"
	const repoID = "repository:r_7384abcd"

	joinFragments := []string{
		"FROM fact_records",
		"JOIN fact_records",
		"repo.fact_kind = 'repository'",
		"NOT repo.is_tombstone",
		"fact_kind = 'reducer_workload_identity'",
		"NOT wid.is_tombstone",
		"repo.payload->>'name'",
	}

	t.Run("id key resolves to repository payload name", func(t *testing.T) {
		t.Parallel()

		db := openContentReaderTestDB(t, []contentReaderQueryResult{{
			columns: []string{"name"},
			rows: [][]driver.Value{
				{"pkg-display"},
			},
			queryContainsInOrder: joinFragments,
			wantArgs:             []driver.Value{scopeID},
		}})

		names, err := NewContentReader(db).repositoryWorkloadNames(context.Background(), scopeID)
		if err != nil {
			t.Fatalf("repositoryWorkloadNames() error = %v", err)
		}
		if want := []string{"pkg-display"}; !reflect.DeepEqual(names, want) {
			t.Fatalf("repositoryWorkloadNames() = %v, want %v (never %q)", names, want, "repository:"+repoID)
		}
		for _, name := range names {
			if len(name) >= 11 && name[0:11] == "repository:" {
				t.Fatalf("repositoryWorkloadNames() = %v, contains a repository id, want display names", names)
			}
		}
	})

	t.Run("tombstoned repository fact yields empty", func(t *testing.T) {
		t.Parallel()

		db := openContentReaderTestDB(t, []contentReaderQueryResult{{
			columns:              []string{"name"},
			rows:                 [][]driver.Value{},
			queryContainsInOrder: joinFragments,
			wantArgs:             []driver.Value{scopeID},
		}})

		names, err := NewContentReader(db).repositoryWorkloadNames(context.Background(), scopeID)
		if err != nil {
			t.Fatalf("repositoryWorkloadNames() error = %v", err)
		}
		if len(names) != 0 {
			t.Fatalf("repositoryWorkloadNames() = %v, want empty when the repository fact is tombstoned", names)
		}
	})

	t.Run("no workload identity yields empty", func(t *testing.T) {
		t.Parallel()

		db := openContentReaderTestDB(t, []contentReaderQueryResult{{
			columns:              []string{"name"},
			rows:                 [][]driver.Value{},
			queryContainsInOrder: joinFragments,
			wantArgs:             []driver.Value{scopeID},
		}})

		names, err := NewContentReader(db).repositoryWorkloadNames(context.Background(), scopeID)
		if err != nil {
			t.Fatalf("repositoryWorkloadNames() error = %v", err)
		}
		if len(names) != 0 {
			t.Fatalf("repositoryWorkloadNames() = %v, want empty without workload identity", names)
		}
	})
}
