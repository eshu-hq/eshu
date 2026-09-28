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
// reader of the workload_identity follow-up key (#7316). The git collector now
// builds that key from the repository fact name, so in dependency mode a
// display name such as "pkg-display" arrives as "workload:pkg-display" and the
// repository summary must report exactly "pkg-display" (the name the workload
// node id is also derived from), not the checkout directory and not an id.
func TestRepositoryWorkloadNamesReturnDisplayNamedWorkloads(t *testing.T) {
	t.Parallel()

	const scopeID = "git-repository-scope:repository:r_7316abcd"
	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns: []string{"entity_key"},
		rows: [][]driver.Value{
			{"workload:group:artifact"},
			{"workload:pkg-display"},
		},
		queryContainsInOrder: []string{"FROM fact_records", "fact_kind = 'reducer_workload_identity'"},
		wantArgs:             []driver.Value{scopeID},
	}})

	names, err := NewContentReader(db).repositoryWorkloadNames(context.Background(), scopeID)
	if err != nil {
		t.Fatalf("repositoryWorkloadNames() error = %v", err)
	}
	if want := []string{"group:artifact", "pkg-display"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("repositoryWorkloadNames() = %v, want %v", names, want)
	}
}
