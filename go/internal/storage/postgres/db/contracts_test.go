// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package db_test

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// The root concrete adapters implement the hoisted contracts structurally.
// These declarations fail compilation if a method set ever drifts.
var (
	_ db.Queryer                        = postgres.SQLDB{}
	_ db.Executor                       = postgres.SQLDB{}
	_ db.ExecQueryer                    = postgres.SQLDB{}
	_ db.Beginner                       = postgres.SQLDB{}
	_ db.ReadOnlyRepeatableReadBeginner = postgres.SQLDB{}
	_ db.ExecQueryer                    = postgres.SQLTx{}
	_ db.Transaction                    = postgres.SQLTx{}
	_ db.Queryer                        = postgres.SQLQueryer{}
)

// TestRootAdaptersSatisfyContracts keeps the compile-time assertions above
// reachable from `go test -list` and pins the SQLDB.Begin shape at runtime
// so a signature drift that somehow still satisfies db.Beginner is caught.
func TestRootAdaptersSatisfyContracts(t *testing.T) {
	t.Parallel()
	m, ok := reflect.TypeFor[postgres.SQLDB]().MethodByName("Begin")
	if !ok {
		t.Fatal("SQLDB.Begin method not found")
	}
	if got := m.Type.NumOut(); got != 2 {
		t.Fatalf("SQLDB.Begin outputs = %d, want 2", got)
	}
}
