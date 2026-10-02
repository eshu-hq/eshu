// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"reflect"
	"testing"
)

// TestRepositoryWorkloadNamesReturnDisplayNamedWorkloads pins the query-side
// reader of the workload_identity follow-up key (#7316, rekeyed by #7384).
// Workload_identity keys are `workload:` plus the repository ID, so the
// repository summary resolves the display name from the repository fact
// payload instead of the key suffix: a colon-bearing name such as
// "group:artifact" must come back verbatim, not split and not an id.
func TestRepositoryWorkloadNamesReturnDisplayNamedWorkloads(t *testing.T) {
	t.Parallel()

	const scopeID = "git-repository-scope:repository:r_7316abcd"
	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns: []string{"name"},
		rows: [][]driver.Value{
			{"group:artifact"},
		},
		queryContainsInOrder: []string{
			"FROM fact_records",
			"fact_kind = 'reducer_workload_identity'",
			"repo.payload->>'name'",
		},
		wantArgs: []driver.Value{scopeID},
	}})

	names, err := NewContentReader(db).repositoryWorkloadNames(context.Background(), scopeID)
	if err != nil {
		t.Fatalf("repositoryWorkloadNames() error = %v", err)
	}
	if want := []string{"group:artifact"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("repositoryWorkloadNames() = %v, want %v", names, want)
	}
}
