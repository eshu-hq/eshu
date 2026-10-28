// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package db_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// frozenReadStore pins the exact method set behind the Postgres ReadStore
// reader surface, which querycontract.CoreReadContractsVersion versions as
// part of the #8872 eshu-api read-contract set. A renamed, added, removed,
// or re-signed method fails here and must bump that version in the same
// diff, so the extraction boundary cannot drift silently. The pin lives in
// this package because querycontract stays dependency-neutral and must not
// import the Postgres adapter surface.
var frozenReadStore = []string{
	"BeginReadOnlySnapshot func(context.Context) (db.ReadTransaction, error)",
	"QueryContext func(context.Context, string, ...interface {}) (db.Rows, error)",
	"QueryRowContext func(context.Context, string, ...interface {}) db.Row",
}

// TestReadStoreContractIsVersioned proves the ReadStore half of the versioned
// core read-contract set: the frozen method set above plus the shared version
// constant. TestCoreReadContractsVersioned in querycontract pins GraphQuery
// and ContentStore; together they enforce the whole set the version names.
func TestReadStoreContractIsVersioned(t *testing.T) {
	t.Parallel()

	if querycontract.CoreReadContractsVersion != "v0" {
		t.Errorf("CoreReadContractsVersion = %q, want %q: bump it deliberately with the read-port change", querycontract.CoreReadContractsVersion, "v0")
	}
	rt := reflect.TypeOf((*db.ReadStore)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	if len(got) != len(frozenReadStore) {
		t.Fatalf("ReadStore method set = %q (len %d), want frozen %q (len %d): bump %s with the read-port change", got, len(got), frozenReadStore, len(frozenReadStore), querycontract.CoreReadContractsVersion)
	}
	for i := range got {
		if got[i] != frozenReadStore[i] {
			t.Errorf("ReadStore method set differs at index %d: got %q, want %q: bump %s with the read-port change", i, got[i], frozenReadStore[i], querycontract.CoreReadContractsVersion)
		}
	}
}
